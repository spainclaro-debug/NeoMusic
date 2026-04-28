package main

import (
    "crypto/md5"
    "context"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/dhowden/tag"
)

type Song struct {
    ID       string `json:"id"`
    Title    string `json:"title"`
    Artist   string `json:"artist"`
    Album    string `json:"album"`
    Lyrics   string `json:"lyrics"`
    AudioURL string `json:"audio_url"`
    ArtURL   string `json:"art_url"`
}

type Playlist struct {
    ID      string   `json:"id"`
    Name    string   `json:"name"`
    SongIDs []string `json:"song_ids"`
    IsFav   bool     `json:"is_fav"`
}

type Config struct {
    Binding  string `json:"binding"`
    Port     string `json:"port"`
    MusicDir string `json:"music_dir"`
}

var songsList []Song
var artDir = "./tmp_art"
var serverConfig Config
var configPath = "config.json"

var playlistsPath = "playlists.json"
var userPlaylists []Playlist

var songsCachePath = "songs_cache.json"

var manifestPath = "manifest.json"
var swPath = "service-worker.js"

var seenSongIDs map[string]bool

func loadConfig() {
    file, err := os.ReadFile(configPath)
    if err != nil {
        serverConfig = Config{
            Binding:  "0.0.0.0",
            Port:     "1220",
            MusicDir: "/data/data/com.termux/files/home/storage",
        }
        saveConfig()
    } else {
        json.Unmarshal(file, &serverConfig)
        if serverConfig.MusicDir == "" {
            serverConfig.MusicDir = "/data/data/com.termux/files/home/storage"
            saveConfig()
        }
    }
}

func saveConfig() {
    data, _ := json.MarshalIndent(serverConfig, "", "  ")
    os.WriteFile(configPath, data, 0644)
}

func loadPlaylists() {
    file, err := os.ReadFile(playlistsPath)
    if err == nil {
        json.Unmarshal(file, &userPlaylists)
        for i := range userPlaylists {
            if userPlaylists[i].SongIDs == nil {
                userPlaylists[i].SongIDs = []string{}
            }
        }
    } else {
        favFile, err := os.ReadFile("favorites.json")
        var favs []string
        if err == nil {
            json.Unmarshal(favFile, &favs)
            if favs == nil {
                favs = []string{}
            }
        } else {
            favs = []string{}
        }
        
        userPlaylists = []Playlist{
            {ID: "fav_root", Name: "Favorites", SongIDs: favs, IsFav: true},
        }
        savePlaylists()
    }
}

func savePlaylists() {
    data, _ := json.MarshalIndent(userPlaylists, "", "  ")
    os.WriteFile(playlistsPath, data, 0644)
}

func loadSongsCache() bool {
    file, err := os.ReadFile(songsCachePath)
    if err == nil {
        err = json.Unmarshal(file, &songsList)
        if err == nil && len(songsList) > 0 {
            return true
        }
    }
    return false
}

func saveSongsCache() {
    data, _ := json.MarshalIndent(songsList, "", "  ")
    os.WriteFile(songsCachePath, data, 0644)
}

func initPWA() {
    if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
        manifestContent := `{
    "name": "NeoMusic",
    "short_name": "NeoMusic",
    "start_url": "/",
    "display": "standalone",
    "background_color": "#121212",
    "theme_color": "#2979ff",
    "icons": [
        {
            "src": "/icon.png",
            "sizes": "512x512",
            "type": "image/png",
            "purpose": "any maskable"
        }
    ]
}`
        os.WriteFile(manifestPath, []byte(manifestContent), 0644)
    }

    os.WriteFile(swPath, swCode, 0644)
}

func main() {
    loadConfig()
    loadPlaylists()
    initPWA()

    os.MkdirAll(artDir, os.ModePerm)
    os.WriteFile(filepath.Join(artDir, ".nomedia"), []byte(""), 0644)

    if loadSongsCache() {
        fmt.Printf("Loaded %d songs from cache.\n", len(songsList))
    } else {
        fmt.Println("First run or cache missing. Scanning music files...")
        scanMusic()
    }

    // Channel to trigger server restart
    restartCh := make(chan struct{})

    // Register all HTTP handlers
    registerHandlers(restartCh)

    // Start the server in a restart loop
    var currentServer *http.Server
    for {
        addr := fmt.Sprintf("%s:%s", serverConfig.Binding, serverConfig.Port)
        currentServer = startServer(addr)

        // Wait for restart signal
        <-restartCh

        // Graceful shutdown with a 5‑second timeout
        fmt.Println("Restarting server with new configuration...")
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        if err := currentServer.Shutdown(ctx); err != nil {
            log.Printf("Server shutdown error: %v", err)
        }
        cancel()
    }
}

func registerHandlers(restartCh chan struct{}) {
    http.Handle("/music/", http.StripPrefix("/music/", http.FileServer(http.Dir(serverConfig.MusicDir))))
    http.Handle("/art/", http.StripPrefix("/art/", http.FileServer(http.Dir(artDir))))

    http.HandleFunc("/icon.png", func(w http.ResponseWriter, r *http.Request) {
        http.ServeFile(w, r, "icon.png")
    })

    http.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        http.ServeFile(w, r, manifestPath)
    })

    http.HandleFunc("/service-worker.js", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/javascript")
        http.ServeFile(w, r, swPath)
    })

    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/html")
        w.Write(indexHtmlCode)
    })

    http.HandleFunc("/api/songs", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Access-Control-Allow-Origin", "*")
        json.NewEncoder(w).Encode(songsList)
    })

    http.HandleFunc("/api/playlists", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Access-Control-Allow-Origin", "*")

        if r.Method == "GET" {
            json.NewEncoder(w).Encode(userPlaylists)
            return
        }

        if r.Method == "POST" {
            var updatedPlaylists []Playlist
            if err := json.NewDecoder(r.Body).Decode(&updatedPlaylists); err == nil {
                for i := range updatedPlaylists {
                    if updatedPlaylists[i].SongIDs == nil {
                        updatedPlaylists[i].SongIDs = []string{}
                    }
                }
                userPlaylists = updatedPlaylists
                savePlaylists()
                w.WriteHeader(http.StatusOK)
                w.Write([]byte(`{"status":"success"}`))
            } else {
                w.WriteHeader(http.StatusBadRequest)
            }
        }
    })

    http.HandleFunc("/api/rescan", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        scanMusic()
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"status":"success"}`))
    })

    http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Header().Set("Access-Control-Allow-Origin", "*")

        if r.Method == "GET" {
            json.NewEncoder(w).Encode(serverConfig)
            return
        }

        if r.Method == "POST" {
            var newConfig Config
            if err := json.NewDecoder(r.Body).Decode(&newConfig); err == nil {
                oldDir := serverConfig.MusicDir
                serverConfig = newConfig
                saveConfig()

                if oldDir != serverConfig.MusicDir {
                    scanMusic()
                }

                // Signal the main loop to restart the server
                go func() {
                    restartCh <- struct{}{}
                }()

                w.WriteHeader(http.StatusOK)
                w.Write([]byte(`{"status":"success"}`))
            } else {
                w.WriteHeader(http.StatusBadRequest)
            }
        }
    })
}

func startServer(addr string) *http.Server {
    srv := &http.Server{
        Addr:         addr,
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout:  60 * time.Second,
    }

    fmt.Println("======================================")
    fmt.Println(" NeoMusic Server Successfully Started")
    fmt.Printf(" Open your browser to: http://localhost:%s\n", serverConfig.Port)
    fmt.Println(" Press Ctrl+C to stop.")
    fmt.Println("======================================")

    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("ListenAndServe(): %v", err)
        }
    }()

    return srv
}

func scanMusic() {
    songsList = []Song{}
    seenSongIDs = make(map[string]bool)
    walkAndScan(serverConfig.MusicDir)
    saveSongsCache()
    fmt.Printf("Scanned %d songs successfully and updated cache.\n", len(songsList))
}

func isAudioFile(filename string) bool {
    ext := strings.ToLower(filepath.Ext(filename))
    return ext == ".mp3" || ext == ".flac" || ext == ".m4a" || ext == ".ogg" || ext == ".wav"
}

func walkAndScan(dir string) {
    entries, err := os.ReadDir(dir)
    if err != nil {
        return
    }

    for _, entry := range entries {
        path := filepath.Join(dir, entry.Name())
        info, err := os.Stat(path)
        if err != nil {
            continue
        }

        if info.IsDir() {
            if strings.HasPrefix(info.Name(), ".") {
                continue
            }
            walkAndScan(path)
        } else if isAudioFile(info.Name()) {
            processFile(path, info)
        }
    }
}

func processFile(path string, info os.FileInfo) {
    f, err := os.Open(path)
    if err != nil {
        return
    }
    defer f.Close()

    m, err := tag.ReadFrom(f)
    if err != nil {
    }

    title := ""
    artist := "Unknown Artist"
    album := "Unknown Album"
    lyrics := ""
    artURL := ""
    
    var picData []byte
    picExt := "jpg"

    if m != nil {
        title = m.Title()
        artist = m.Artist()
        album = m.Album()
        lyrics = m.Lyrics()

        if m.Picture() != nil {
            pic := m.Picture()
            picData = pic.Data
            if pic.MIMEType == "image/png" {
                picExt = "png"
            }
        }
    }

    if title == "" {
        title = info.Name()
    }

    uniqueKey := fmt.Sprintf("%s|%s|%s|%d", title, artist, album, info.Size())
    hash := md5.Sum([]byte(uniqueKey))
    id := hex.EncodeToString(hash[:])

    if seenSongIDs[id] {
        return
    }
    seenSongIDs[id] = true

    if picData != nil {
        artFilename := fmt.Sprintf("%s.%s", id, picExt)
        artPath := filepath.Join(artDir, artFilename)

        if _, err := os.Stat(artPath); os.IsNotExist(err) {
            os.WriteFile(artPath, picData, 0644)
        }
        artURL = "/art/" + artFilename
    }

    relPath, err := filepath.Rel(serverConfig.MusicDir, path)
    if err != nil {
        return
    }

    song := Song{
        ID:       id,
        Title:    title,
        Artist:   artist,
        Album:    album,
        Lyrics:   lyrics,
        AudioURL: "/music/" + filepath.ToSlash(relPath),
        ArtURL:   artURL,
    }
    songsList = append(songsList, song)
}

var swCode = []byte(`const CACHE_NAME = 'neomusic-v13';
const STATIC_ASSETS = ['/', '/manifest.json', '/icon.png'];

self.addEventListener('install', (event) => {
    event.waitUntil(
        caches.open(CACHE_NAME).then((cache) => cache.addAll(STATIC_ASSETS))
    );
    self.skipWaiting();
});

self.addEventListener('activate', (event) => {
    event.waitUntil(
        caches.keys().then(keys => Promise.all(
            keys.map(key => {
                if (key !== CACHE_NAME) return caches.delete(key);
            })
        ))
    );
    self.clients.claim();
});

self.addEventListener('fetch', (event) => {
    if (event.request.method !== 'GET') return;

    const url = new URL(event.request.url);

    if (url.pathname.startsWith('/api/')) {
        event.respondWith(
            fetch(event.request)
                .then(res => {
                    const resClone = res.clone();
                    caches.open(CACHE_NAME).then(cache => cache.put(event.request, resClone));
                    return res;
                })
                .catch(() => caches.match(event.request))
        );
        return;
    }

    if (url.pathname.startsWith('/music/') || url.pathname.startsWith('/art/')) {
        event.respondWith(
            caches.match(event.request).then(cachedRes => {
                if (cachedRes) return cachedRes; 

                return fetch(event.request).then(networkRes => {
                    if (url.pathname.startsWith('/art/')) {
                        const resClone = networkRes.clone();
                        caches.open(CACHE_NAME).then(cache => cache.put(event.request, resClone));
                    }
                    return networkRes;
                });
            })
        );
        return;
    }

    event.respondWith(
        caches.match(event.request).then(cachedRes => {
            const fetchPromise = fetch(event.request).then(networkRes => {
                caches.open(CACHE_NAME).then(cache => cache.put(event.request, networkRes.clone()));
                return networkRes;
            }).catch(() => {});
            return cachedRes || fetchPromise;
        })
    );
});
`)

var indexHtmlCode = []byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no">
    <title>NeoMusic</title>
    <link rel="icon" href="/icon.png" type="image/png">
    <link rel="apple-touch-icon" href="/icon.png">
    <link rel="manifest" href="/manifest.json">
    <meta name="theme-color" content="#121212">
    <style>
        :root {
            --bg-base: #121212;
            --bg-surface: rgba(24, 24, 24, 0.6); 
            --bg-elevated: rgba(40, 40, 40, 0.7);
            --text-primary: #ffffff;
            --text-secondary: #e0e0e0;
            --accent: #2979ff;       
            --accent-hover: #448aff;
            --art-size: 450px;
            --title-size: 26px;
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
            -webkit-tap-highlight-color: transparent;
        }

        body {
            font-family: 'Circular', -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            background-color: var(--bg-base);
            color: var(--text-primary);
            display: flex;
            justify-content: center;
            min-height: 100vh;
            min-height: -webkit-fill-available;
            padding: 20px;
            padding-bottom: max(20px, env(safe-area-inset-bottom));
            padding-top: max(20px, env(safe-area-inset-top));
            position: relative;
        }

        .bg-blur {
            position: fixed; top: 0; left: 0; right: 0; bottom: 0;
            width: 100%; height: 100%; z-index: -1;
            background-size: cover; background-position: center;
            filter: blur(25px) brightness(0.35); transform: scale(1.1); 
            transition: background-image 0.5s ease-in-out;
        }

        .app-container {
            width: 100%; max-width: 450px; display: flex; flex-direction: column;
            height: 100%; transition: max-width 0.3s ease; z-index: 1;
        }

        .top-bar { position: relative; display: flex; gap: 8px; margin-bottom: 25px; z-index: 100; width: 100%; }
        .search-container { position: relative; flex-grow: 1; }
        .search-bar { width: 100%; height: 44px; padding: 0 20px 0 45px; border-radius: 500px; border: none; background-color: var(--bg-elevated); backdrop-filter: blur(10px); color: var(--text-primary); font-size: 0.95rem; outline: none; transition: background-color 0.2s; }
        .search-bar:focus, .search-bar:hover { background-color: rgba(60, 60, 60, 0.8); }
        .search-icon-svg { position: absolute; left: 15px; top: 50%; transform: translateY(-50%); color: var(--text-secondary); width: 18px; height: 18px; }

        .library-btn { width: 44px; height: 44px; border-radius: 50%; background-color: var(--bg-elevated); backdrop-filter: blur(10px); border: none; color: var(--text-primary); display: flex; align-items: center; justify-content: center; cursor: pointer; transition: all 0.2s; flex-shrink: 0; }
        .library-btn:hover { background-color: rgba(60, 60, 60, 0.8); }
        .library-btn svg { width: 20px; height: 20px; stroke: currentColor; }
        .playlist-active { color: var(--accent); }

        .list-dropdown { position: absolute; top: 54px; left: 0; width: 100%; max-height: 280px; overflow-y: auto; background-color: rgba(30, 30, 30, 0.95); backdrop-filter: blur(15px); border-radius: 8px; box-shadow: 0 8px 24px rgba(0,0,0,0.6); display: none; list-style: none; padding: 8px 0; scrollbar-width: thin; scrollbar-color: rgba(255,255,255,0.2) transparent; }
        .list-dropdown::-webkit-scrollbar { width: 6px; }
        .list-dropdown::-webkit-scrollbar-thumb { background-color: rgba(255,255,255,0.2); border-radius: 10px; }

        .list-dropdown li { padding: 10px 15px; cursor: pointer; color: var(--text-secondary); font-size: 0.95rem; display: flex; flex-direction: row; align-items: center; gap: 12px; border-left: 3px solid transparent; transition: background 0.2s, opacity 0.2s; }
        .list-dropdown li:hover { background-color: rgba(255,255,255,0.1); }
        .list-dropdown li.active-song { border-left-color: var(--accent); background-color: rgba(255, 255, 255, 0.1); }
        .list-dropdown li.active-song span.res-title { color: var(--accent); }

        .pl-hub-name { flex-grow: 1; display: flex; align-items: center; gap: 8px; font-weight: 500;}
        .pl-hub-actions { display: flex; gap: 10px; }
        .pl-hub-btn { background: none; border: none; color: var(--text-secondary); cursor: pointer; padding: 4px; display: flex; align-items: center; transition: color 0.2s; }
        .pl-hub-btn:hover { color: var(--text-primary); }
        .pl-hub-btn svg { width: 16px; height: 16px; stroke: currentColor; }

        .song-options-container { display: flex; align-items: center; justify-content: center; }
        .song-options-btn { background: none; border: none; color: var(--text-secondary); width: 32px; height: 32px; border-radius: 50%; display: flex; align-items: center; justify-content: center; cursor: pointer; transition: color 0.2s, background 0.2s; }
        .song-options-btn:hover { color: var(--text-primary); background: rgba(255, 255, 255, 0.1); }

        .global-song-menu { position: fixed; background-color: #242424; border: 1px solid rgba(255,255,255,0.1); border-radius: 8px; box-shadow: 0 5px 25px rgba(0,0,0,0.8); display: none; flex-direction: column; z-index: 10000; min-width: 170px; overflow: hidden; }
        .global-song-menu.show { display: flex; }
        .global-song-menu button { background: none; border: none; color: var(--text-primary); padding: 14px 18px; text-align: left; cursor: pointer; font-size: 0.9rem; font-family: inherit; transition: background 0.2s; white-space: nowrap; }
        .global-song-menu button:hover { background-color: rgba(255,255,255,0.1); }

        .song-info { display: flex; flex-direction: column; flex-grow: 1; overflow: hidden; }
        .song-info span.res-title { color: var(--text-primary); font-weight: 500; margin-bottom: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .song-info span.res-artist { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

        .player-content { display: flex; flex-direction: column; flex-grow: 1; }
        .player-left { width: 100%; display: flex; justify-content: center; align-items: center; }
        .player-right { width: 100%; display: flex; flex-direction: column; min-width: 0; }

        .art-container { width: var(--art-size); height: var(--art-size); max-width: 100%; aspect-ratio: 1/1; flex-shrink: 0; border-radius: 12px; background-color: rgba(0, 0, 0, 0.4); box-shadow: 0 15px 35px rgba(0,0,0,0.5); margin-bottom: 25px; display: flex; align-items: center; justify-content: center; overflow: hidden; position: relative; background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23b3b3b3' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M9 18V5l12-2v13'%3E%3C/path%3E%3Ccircle cx='6' cy='18' r='3'%3E%3C/circle%3E%3Ccircle cx='18' cy='16' r='3'%3E%3C/circle%3E%3C/svg%3E"); background-size: 60px; background-position: center; background-repeat: no-repeat; }
        .art-container img { position: absolute; top: 0; left: 0; width: 100%; height: 100%; object-fit: cover; display: none; z-index: 5; transition: opacity 0.3s ease; }
        .lyrics-container { position: absolute; top: 0; left: 0; right: 0; bottom: 0; width: 100%; height: 100%; background-color: rgba(10, 10, 10, 0.85); backdrop-filter: blur(10px); padding: 20px; overflow-y: auto; text-align: center; line-height: 1.8; font-size: 0.95rem; color: var(--text-primary); display: none; z-index: 10; scrollbar-width: thin; scrollbar-color: rgba(255,255,255,0.2) transparent; }
        .lyrics-container::-webkit-scrollbar { width: 6px; }
        .lyrics-container::-webkit-scrollbar-thumb { background-color: rgba(255,255,255,0.2); border-radius: 10px; }
        .art-container.show-lyrics .lyrics-container { display: block; }

        #heartBtn, #downloadBtn { position: absolute; bottom: 15px; z-index: 20; background: rgba(18, 18, 18, 0.6); backdrop-filter: blur(8px); border: 1px solid rgba(255, 255, 255, 0.1); color: var(--text-primary); width: 44px; height: 44px; border-radius: 50%; display: flex; align-items: center; justify-content: center; transition: transform 0.1s, background-color 0.2s, color 0.2s; cursor: pointer; box-shadow: 0 4px 10px rgba(0,0,0,0.4); }
        #heartBtn { right: 15px; }
        #downloadBtn { left: 15px; }
        #heartBtn:hover, #downloadBtn:hover { background: rgba(40, 40, 40, 0.8); transform: scale(1.05); }
        #heartBtn:active, #downloadBtn:active { transform: scale(0.95); }
        #heartBtn svg, #downloadBtn svg { width: 22px; height: 22px; fill: none; stroke: currentColor; stroke-width: 2; }
        #heartEmpty { fill: transparent !important; }
        #heartFilled { display: none; }
        #heartBtn.heart-active { color: var(--accent); }
        #heartBtn.heart-active #heartEmpty { display: none; }
        #heartBtn.heart-active #heartFilled { display: block; fill: currentColor; }
        #downloadBtn.downloaded { color: var(--accent); }

        .info-section { display: flex; flex-direction: column; align-items: center; margin-bottom: 20px; width: 100%; min-width: 0; }
        .title { width: 100%; text-align: center; font-size: var(--title-size); font-weight: 700; color: var(--text-primary); margin-bottom: 4px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; text-shadow: 0 2px 4px rgba(0,0,0,0.5); transition: font-size 0.2s ease; }
        .artist { width: 100%; text-align: center; font-size: 1.1rem; color: var(--text-secondary); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; text-shadow: 0 1px 3px rgba(0,0,0,0.5); }
        .lyrics-toggle { background: transparent; border: 1px solid rgba(255,255,255,0.3); color: var(--text-primary); border-radius: 20px; padding: 5px 16px; font-size: 0.75rem; font-weight: 600; cursor: pointer; transition: all 0.2s; text-transform: uppercase; letter-spacing: 1px; margin-top: 15px; backdrop-filter: blur(5px); }
        .lyrics-toggle:hover { border-color: var(--text-primary); background-color: rgba(255,255,255,0.1); }
        .lyrics-toggle.active { background: var(--text-primary); color: #121212; border-color: var(--text-primary); }

        .progress-wrapper { display: flex; flex-direction: column; margin-bottom: 20px; width: 100%; }
        .visualizer-container { position: relative; width: 100%; height: 50px; cursor: pointer; margin-bottom: 5px; display: flex; align-items: center; }
        .visualizer-container canvas { width: 100%; height: 100%; z-index: 10; }
        .time-labels { display: flex; justify-content: space-between; font-size: 0.8rem; font-weight: 500; color: var(--text-secondary); font-variant-numeric: tabular-nums; text-shadow: 0 1px 2px rgba(0,0,0,0.5); }

        .controls { display: flex; justify-content: space-between; align-items: center; }
        .btn { background: none; border: none; color: var(--text-primary); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: transform 0.1s, opacity 0.2s; padding: 10px; border-radius: 50%; opacity: 0.8; filter: drop-shadow(0 2px 3px rgba(0,0,0,0.3)); }
        .btn:hover { opacity: 1; background-color: rgba(255, 255, 255, 0.1); }
        .btn:active { transform: scale(0.9); }
        .btn svg { width: 22px; height: 22px; fill: currentColor; stroke: none; }
        .btn.active-state { color: var(--accent); opacity: 1; }

        .shuffle-icon-btn { background: none; border: none; padding: 8px; color: #b3b3b3; cursor: pointer; display: inline-flex; align-items: center; justify-content: center; transition: color 0.25s ease; outline: none; }
        .shuffle-icon-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 4px; }
        .shuffle-icon-btn:hover { color: #ffffff; }
        .shuffle-icon-btn.active-state { color: var(--accent); }
        .shuffle-icon-btn svg { width: 20px; height: 20px; display: block; }
        .repeat-icon-btn { background: none; border: none; padding: 8px; color: #b3b3b3; cursor: pointer; display: inline-flex; align-items: center; justify-content: center; transition: color 0.25s ease; outline: none; position: relative; }
        .repeat-icon-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 4px; }
        .repeat-icon-btn:hover { color: #ffffff; }
        .repeat-icon-btn.active { color: var(--accent); }
        .repeat-icon-btn svg { width: 24px; height: 24px; display: block; }
        .repeat-dot { display: none; position: absolute; bottom: 0px; left: 50%; transform: translateX(-50%); width: 5px; height: 5px; background: var(--accent); border-radius: 50%; }
        .repeat-icon-btn.repeat-one .repeat-dot { display: block; }
        .play-btn { background-color: var(--text-primary); color: #121212; width: 64px; height: 64px; border-radius: 50%; padding: 0; border: none; cursor: pointer; display: flex; align-items: center; justify-content: center; transition: transform 0.1s, background-color 0.2s; box-shadow: 0 5px 15px rgba(0,0,0,0.4); }
        .play-btn:hover { transform: scale(1.05); }
        .play-btn:active { transform: scale(0.95); }
        .play-btn svg { width: 28px; height: 28px; fill: currentColor; stroke: none; }
        .play-icon { transform: translateX(2px); }
        .pause-icon { display: none; }
        .playing .play-icon { display: none; }
        .playing .pause-icon { display: block; transform: none; }

        .volume-wrapper { display: flex; align-items: center; gap: 15px; width: 100%; margin-top: 15px; }
        .ui-slider { -webkit-appearance: none; width: 100%; height: 5px; background: rgba(255, 255, 255, 0.2); border-radius: 3px; outline: none; cursor: pointer; }
        .ui-slider::-webkit-slider-thumb { -webkit-appearance: none; appearance: none; width: 16px; height: 16px; border-radius: 50%; background: #ffffff; cursor: pointer; box-shadow: 0 0 5px rgba(0,0,0,0.5); transition: transform 0.1s; }
        .ui-slider::-webkit-slider-thumb:active { transform: scale(1.3); }
        .volume-btn { background: none; border: none; color: var(--text-secondary); cursor: pointer; display: flex; align-items: center; justify-content: center; padding: 5px; transition: opacity 0.2s, transform 0.1s; }
        .volume-btn:hover { color: var(--text-primary); }
        .volume-btn:active { transform: scale(0.9); }
        .volume-btn svg { width: 20px; height: 20px; fill: none; stroke: currentColor; stroke-width: 2; }
        .vol-icon-on { display: block; }
        .vol-icon-off { display: none; }
        .muted .vol-icon-on { display: none; }
        .muted .vol-icon-off { display: block; }

        .modal-bg { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.6); z-index: 15000; display: none; justify-content: center; align-items: center; backdrop-filter: blur(5px); }
        .modal-content { background: var(--bg-elevated); padding: 25px; border-radius: 15px; width: 340px; max-width: 90%; box-shadow: 0 10px 30px rgba(0,0,0,0.5); border: 1px solid rgba(255,255,255,0.1); }
        .modal-btn-group { display: flex; gap: 10px; margin-top: 20px; }
        .modal-btn { flex: 1; padding: 10px; border-radius: 8px; border: none; font-weight: 600; cursor: pointer; transition: 0.2s; }
        .btn-cancel { background: rgba(255,255,255,0.1); color: white; }
        .btn-primary { background: var(--accent); color: white; }
        
        .pl-select-list { list-style: none; margin: 15px 0; max-height: 250px; overflow-y: auto; scrollbar-width: thin; }
        .pl-select-list li { display: flex; justify-content: space-between; align-items: center; padding: 10px; border-bottom: 1px solid rgba(255,255,255,0.1); font-size: 0.95rem; }
        .pl-select-list li:last-child { border-bottom: none; }
        .pl-add-btn { background: var(--accent); border: none; color: white; padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 0.8rem; font-weight: bold; }
        .pl-remove-btn { background: rgba(244, 67, 54, 0.8); border: none; color: white; padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 0.8rem; font-weight: bold; }

        @media (min-width: 768px) {
            body { align-items: center; }
            :root { --title-size: 40px; }
            .app-container { max-width: 900px; justify-content: center; }
            .player-content { flex-direction: row; align-items: center; gap: 60px; }
            .player-left { flex: 1; max-width: var(--art-size); display: flex; justify-content: center; align-items: center; }
            .player-right { flex: 1.2; justify-content: center; }
            .art-container { margin-bottom: 0; }
            .info-section { align-items: flex-start; margin-bottom: 30px; }
            .title { text-align: left; }
            .artist { text-align: left; font-size: 1.3rem; }
            .lyrics-toggle { margin-top: 15px; }
            .list-dropdown { max-height: 400px; }
            .volume-wrapper { margin-top: 25px; }
        }
    </style>
</head>
<body>

    <div class="bg-blur" id="bgBlur"></div>

    <div class="app-container">
        
        <div class="top-bar">
            <div class="search-container">
                <svg class="search-icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
                <input type="text" id="searchInput" class="search-bar" placeholder="Find a song...">
            </div>
            
            <button id="playlistsBtn" class="library-btn" title="Your Playlists">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>
                </svg>
            </button>

            <button id="queueBtn" class="library-btn" title="Current Queue">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="8" y1="6" x2="21" y2="6"></line>
                    <line x1="8" y1="12" x2="21" y2="12"></line>
                    <line x1="8" y1="18" x2="15" y2="18"></line>
                    <circle cx="4" cy="6" r="1"></circle>
                    <circle cx="4" cy="12" r="1"></circle>
                    <circle cx="4" cy="18" r="1"></circle>
                    <polygon points="19 16 23 19 19 22"></polygon>
                </svg>
            </button>

            <button id="libraryBtn" class="library-btn" title="View entire library">
                <svg viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <line x1="8" y1="6" x2="21" y2="6"></line><line x1="8" y1="12" x2="21" y2="12"></line><line x1="8" y1="18" x2="21" y2="18"></line>
                    <line x1="3" y1="6" x2="3.01" y2="6"></line><line x1="3" y1="12" x2="3.01" y2="12"></line><line x1="3" y1="18" x2="3.01" y2="18"></line>
                </svg>
            </button>

            <button id="settingsBtn" class="library-btn" title="Settings">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="12" cy="12" r="3"></circle>
                    <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
                </svg>
            </button>

            <ul id="listDropdown" class="list-dropdown"></ul>
        </div>

        <div class="player-content">
            
            <div class="player-left">
                <div class="art-container" id="artContainer">
                    <img id="album-art" src="" alt="Album Art">
                    <div class="lyrics-container" id="lyricsBox">
                        Loading lyrics...
                    </div>
                    <button id="downloadBtn" title="Download for Offline">
                        <svg viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                            <polyline points="7 10 12 15 17 10"></polyline>
                            <line x1="12" y1="15" x2="12" y2="3"></line>
                        </svg>
                    </button>
                    <button id="heartBtn" title="Add to Favorites">
                        <svg id="heartEmpty" viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
                        </svg>
                        <svg id="heartFilled" viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
                            <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
                        </svg>
                    </button>
                </div>
            </div>

            <div class="player-right">
                <div class="info-section">
                    <div class="title" id="title">Loading Library...</div>
                    <div class="artist" id="artist">Please wait</div>
                    <button id="lyricsBtn" class="lyrics-toggle">Show Lyrics</button>
                </div>

                <div class="progress-wrapper">
                    <div class="visualizer-container" id="visContainer">
                        <canvas id="visCanvas" width="1000" height="100"></canvas>
                    </div>
                    <div class="time-labels">
                        <span id="currTime">0:00</span>
                        <span id="durTime">0:00</span>
                    </div>
                </div>

                <div class="controls">
                    <button class="shuffle-icon-btn" id="shuffleBtn" aria-label="Shuffle is off" title="Shuffle: OFF">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <polyline points="16 3 21 3 21 8" />
                            <line x1="4" y1="20" x2="21" y2="3" />
                            <polyline points="21 16 21 21 16 21" />
                            <line x1="15" y1="15" x2="21" y2="21" />
                            <line x1="4" y1="4" x2="9" y2="9" />
                        </svg>
                    </button>
                    <button class="btn" id="prevBtn">
                        <svg viewBox="0 0 16 16"><path d="M3.3 1a.7.7 0 01.7.7v5.15l9.95-5.744a.7.7 0 011.05.606v12.575a.7.7 0 01-1.05.607L4 9.149V14.3a.7.7 0 01-.7.7H1.7a.7.7 0 01-.7-.7V1.7a.7.7 0 01.7-.7h1.6z"></path></svg>
                    </button>
                    <button class="play-btn" id="playBtn">
                        <svg class="play-icon" viewBox="0 0 24 24"><path d="M7.05 3.606l13.49 7.788a.7.7 0 010 1.212L7.05 20.394A.7.7 0 016 19.788V4.212a.7.7 0 011.05-.606z"></path></svg>
                        <svg class="pause-icon" viewBox="0 0 24 24"><path d="M5.7 3a.7.7 0 00-.7.7v16.6a.7.7 0 00.7.7h2.6a.7.7 0 00.7-.7V3.7a.7.7 0 00-.7-.7H5.7zm10 0a.7.7 0 00-.7.7v16.6a.7.7 0 00.7.7h2.6a.7.7 0 00.7-.7V3.7a.7.7 0 00-.7-.7h-2.6z"></path></svg>
                    </button>
                    <button class="btn" id="nextBtn">
                        <svg viewBox="0 0 16 16"><path d="M12.7 1a.7.7 0 00-.7.7v5.15L2.05 1.107A.7.7 0 001 1.712v12.575a.7.7 0 001.05.607L12 9.149V14.3a.7.7 0 00.7.7h1.6a.7.7 0 00.7-.7V1.7a.7.7 0 00-.7-.7h-1.6z"></path></svg>
                    </button>
                    <button class="repeat-icon-btn" id="repeatBtn" aria-label="Repeat is off" title="Repeat: OFF">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <polyline points="17 1 21 5 17 9"></polyline>
                            <path d="M3 11V9a4 4 0 0 1 4-4h14"></path>
                            <polyline points="7 23 3 19 7 15"></polyline>
                            <path d="M21 13v2a4 4 0 0 1-4 4H3"></path>
                        </svg>
                        <span class="repeat-dot" id="repeatDot"></span>
                    </button>
                </div>

                <div class="volume-wrapper">
                    <button class="volume-btn" id="muteBtn" title="Mute/Unmute">
                        <svg class="vol-icon-on" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"></polygon>
                            <path d="M19.07 4.93a10 10 0 0 1 0 14.14"></path>
                            <path d="M15.54 8.46a5 5 0 0 1 0 7.07"></path>
                        </svg>
                        <svg class="vol-icon-off" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"></polygon>
                            <line x1="23" y1="9" x2="17" y2="15"></line>
                            <line x1="17" y1="9" x2="23" y2="15"></line>
                        </svg>
                    </button>
                    <input type="range" class="ui-slider" id="volumeSlider" value="1" min="0" max="1" step="0.01" style="background: linear-gradient(to right, rgb(255, 255, 255) 100%, rgba(255, 255, 255, 0.3) 100%);">
                </div>

            </div>
        </div>

    </div>

    <div id="globalSongMenu" class="global-song-menu">
        <button id="gsmPlaylistBtn">Manage Playlists</button>
        <button id="gsmRemovePlBtn" style="display:none; color: #ff5252;">Remove from Playlist</button>
        <button id="gsmQueueBtn">Add to Queue</button>
        <button id="gsmDlBtn">Download</button>
    </div>

    <div id="addToPlaylistModal" class="modal-bg">
        <div class="modal-content">
            <h2 style="margin-bottom:10px; font-size:1.3rem;">Add to Playlist</h2>
            <ul id="addToPlaylistList" class="pl-select-list"></ul>
            <div class="modal-btn-group">
                <button class="modal-btn btn-cancel" onclick="document.getElementById('addToPlaylistModal').style.display='none'">Done</button>
            </div>
        </div>
    </div>

    <div id="editPlaylistModal" class="modal-bg">
        <div class="modal-content">
            <h2 id="epmTitle" style="margin-bottom:20px; font-size:1.3rem;">Playlist</h2>
            <input type="text" id="epmName" placeholder="Playlist Name" style="width:100%; padding:10px 15px; border-radius:8px; border:1px solid rgba(255,255,255,0.2); background:rgba(0,0,0,0.3); color:white; outline:none; font-family:inherit; margin-bottom:10px;">
            <div class="modal-btn-group">
                <button class="modal-btn btn-cancel" onclick="document.getElementById('editPlaylistModal').style.display='none'">Cancel</button>
                <button class="modal-btn btn-primary" id="epmSave">Save</button>
            </div>
        </div>
    </div>

    <div id="settingsModal" class="modal-bg">
        <div class="modal-content">
            <h2 style="margin-bottom:20px; text-align:center; font-size:1.4rem;">Settings</h2>
            
            <div style="margin-bottom:15px;">
                <label style="display:block; margin-bottom:8px; font-size:0.9rem; color:var(--text-secondary);">Binding (IP)</label>
                <input type="text" id="bindInput" style="width:100%; padding:10px 15px; border-radius:8px; border:1px solid rgba(255,255,255,0.2); background:rgba(0,0,0,0.3); color:white; outline:none; font-family:inherit;">
            </div>
            
            <div style="margin-bottom:15px;">
                <label style="display:block; margin-bottom:8px; font-size:0.9rem; color:var(--text-secondary);">Port</label>
                <input type="text" id="portInput" style="width:100%; padding:10px 15px; border-radius:8px; border:1px solid rgba(255,255,255,0.2); background:rgba(0,0,0,0.3); color:white; outline:none; font-family:inherit;">
            </div>

            <div style="margin-bottom:20px;">
                <label style="display:block; margin-bottom:8px; font-size:0.9rem; color:var(--text-secondary);">Music Directory</label>
                <input type="text" id="dirInput" style="width:100%; padding:10px 15px; border-radius:8px; border:1px solid rgba(255,255,255,0.2); background:rgba(0,0,0,0.3); color:white; outline:none; font-family:inherit;">
            </div>
            
            <hr style="border:0; border-top:1px solid rgba(255,255,255,0.1); margin: 20px 0;">

            <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom: 15px;">
                <span style="font-size:1rem; font-weight:600; color:var(--text-primary);">Appearance</span>
                <button id="resetUIBtn" style="background:none; border:none; color:var(--accent); cursor:pointer; font-size:0.8rem; text-transform:uppercase; font-weight:bold; padding:0;">Reset All</button>
            </div>

            <div style="margin-bottom:15px;">
                <label style="display:block; margin-bottom:8px; font-size:0.9rem; color:var(--text-secondary);">Accent Color (Hex)</label>
                <div style="display:flex; gap:8px; align-items:stretch; height:42px;">
                    <div style="display:flex; flex:1; align-items:center; background:rgba(0,0,0,0.3); border:1px solid rgba(255,255,255,0.2); border-radius:8px; overflow:hidden;">
                        <span style="padding-left:12px; color:var(--text-secondary); font-weight:bold;">#</span>
                        <input type="text" id="accentInput" placeholder="2979ff" style="width:100%; padding:10px; border:none; background:transparent; color:white; outline:none; font-family:inherit;">
                    </div>
                    <button id="applyAccentBtn" style="padding:0 15px; border-radius:8px; border:none; background:var(--accent); color:white; cursor:pointer; font-weight:600; transition:background 0.2s;">Apply</button>
                    <button id="resetAccentBtn" title="Reset Accent Color" style="width:48px; flex-shrink:0; border-radius:8px; border:1px solid rgba(255,255,255,0.2); background:rgba(255,255,255,0.05); color:white; cursor:pointer; display:flex; align-items:center; justify-content:center; transition:0.2s;">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width:18px; height:18px;"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"></path><path d="M3 3v5h5"></path></svg>
                    </button>
                </div>
            </div>

            <div style="margin-bottom:20px;">
                <label style="display:flex; justify-content:space-between; margin-bottom:8px; font-size:0.9rem; color:var(--text-secondary);">
                    <span>Album Art Size</span>
                    <span id="artSizeVal" style="color:var(--accent); font-weight:bold;">450px</span>
                </label>
                <input type="range" class="ui-slider" id="artSizeSlider" min="200" max="600" value="450">
            </div>

            <div style="margin-bottom:25px;">
                <label style="display:flex; justify-content:space-between; margin-bottom:8px; font-size:0.9rem; color:var(--text-secondary);">
                    <span>Title Font Size</span>
                    <span id="titleSizeVal" style="color:var(--accent); font-weight:bold;">26px</span>
                </label>
                <input type="range" class="ui-slider" id="titleSizeSlider" min="16" max="64" value="26">
            </div>

            <div style="margin-bottom:25px; text-align:center;">
                <button id="rescanBtn" style="padding:8px 16px; border-radius:8px; border:1px solid rgba(255,255,255,0.2); background:rgba(255,255,255,0.05); color:white; cursor:pointer; font-weight:500; transition:0.2s; font-size:0.85rem;">⟳ Rescan Library</button>
                <div id="rescanMsg" style="margin-top:8px; font-size:0.8rem; color:var(--accent); display:none;">Rescanning...</div>
            </div>
            
            <div class="modal-btn-group">
                <button id="closeSettings" class="modal-btn btn-cancel">Cancel</button>
                <button id="saveSettings" class="modal-btn btn-primary">Save</button>
            </div>
            
            <div id="settingsMsg" style="margin-top:15px; text-align:center; font-size:0.85rem; color:#4caf50; display:none;">Settings saved!</div>
        </div>
    </div>

    <audio id="audio" crossorigin="anonymous"></audio>

    <script>
        if ('serviceWorker' in navigator) {
            navigator.serviceWorker.register('/service-worker.js')
            .then(reg => console.log('Service Worker registered successfully'))
            .catch(err => console.error('Service Worker registration failed', err));
        }

        const heartSvgIcon = '<svg viewBox="0 0 24 24" style="width:18px;height:18px;fill:var(--accent);stroke:var(--accent);stroke-width:2;stroke-linecap:round;stroke-linejoin:round;flex-shrink:0;"><path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path></svg>';
        const folderSvgIcon = '<svg viewBox="0 0 24 24" style="width:18px;height:18px;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round;flex-shrink:0;"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>';

        let songs = [];
        let playlists = []; 

        let editingPlaylistId = null;

        function savePlaylistsAPI() {
            fetch('/api/playlists', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(playlists)
            });
        }

        function showEditPlaylistModal(plId = null) {
            editingPlaylistId = plId;
            const title = document.getElementById('epmTitle');
            const input = document.getElementById('epmName');
            
            if (plId) {
                const pl = playlists.find(p => p.id === plId);
                title.innerText = "Edit Playlist";
                input.value = pl.name;
            } else {
                title.innerText = "New Playlist";
                input.value = "";
            }
            document.getElementById('editPlaylistModal').style.display = 'flex';
            input.focus();
        }

        document.getElementById('epmSave').onclick = () => {
            const name = document.getElementById('epmName').value.trim();
            if (!name) return;
            
            if (editingPlaylistId) {
                const pl = playlists.find(p => p.id === editingPlaylistId);
                if (pl && !pl.is_fav) pl.name = name;
            } else {
                playlists.push({
                    id: 'pl_' + Date.now(),
                    name: name,
                    song_ids: []
                });
            }
            
            savePlaylistsAPI();
            document.getElementById('editPlaylistModal').style.display = 'none';
            if (currentListMode === 'playlists_root') renderPlaylistsRoot();
        };

        function deletePlaylist(plId, e) {
            e.stopPropagation();
            if(confirm("Are you sure you want to delete this playlist?")) {
                playlists = playlists.filter(p => p.id !== plId);
                savePlaylistsAPI();
                if (currentListMode === 'playlists_root') renderPlaylistsRoot();
            }
        }

        function showAddToPlaylistModal(song) {
            const list = document.getElementById('addToPlaylistList');
            list.innerHTML = '';
            
            playlists.forEach(pl => {
                pl.song_ids = pl.song_ids || [];
                const li = document.createElement('li');
                
                const nameSpan = document.createElement('span');
                nameSpan.style.display = 'flex';
                nameSpan.style.alignItems = 'center';
                nameSpan.style.gap = '8px';
                nameSpan.innerHTML = (pl.is_fav ? heartSvgIcon : folderSvgIcon) + '<span>' + pl.name + '</span>';
                
                const inPl = pl.song_ids.includes(song.id);
                const btn = document.createElement('button');
                btn.className = inPl ? 'pl-remove-btn' : 'pl-add-btn';
                btn.innerText = inPl ? 'Remove' : 'Add';
                
                btn.onclick = () => {
                    if (inPl) {
                        pl.song_ids = pl.song_ids.filter(id => id !== song.id);
                    } else {
                        pl.song_ids.push(song.id);
                    }
                    savePlaylistsAPI();
                    showAddToPlaylistModal(song); 
                    
                    if (pl.is_fav) {
                        let currentPlayingSong = playingFromUserQueue ? userQueue[userQueueIndex] : baseQueue[baseIndex];
                        if (currentPlayingSong && currentPlayingSong.id === song.id) {
                            if (pl.song_ids.includes(song.id)) heartBtn.classList.add('heart-active');
                            else heartBtn.classList.remove('heart-active');
                        }
                    }
                    
                    if (currentListMode === 'playlist_songs' && currentPlaylistId === pl.id) {
                        renderPlaylistSongs(pl.id);
                    }
                };
                
                li.appendChild(nameSpan);
                li.appendChild(btn);
                list.appendChild(li);
            });
            
            document.getElementById('addToPlaylistModal').style.display = 'flex';
        }

        const globalSongMenu = document.getElementById('globalSongMenu');
        const gsmPlaylistBtn = document.getElementById('gsmPlaylistBtn');
        const gsmRemovePlBtn = document.getElementById('gsmRemovePlBtn');
        const gsmQueueBtn = document.getElementById('gsmQueueBtn');
        const gsmDlBtn = document.getElementById('gsmDlBtn');
        
        let currentMenuSong = null;
        let currentMenuIndex = -1;

        gsmPlaylistBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            if (currentMenuSong) {
                globalSongMenu.classList.remove('show');
                showAddToPlaylistModal(currentMenuSong);
            }
        });

        gsmRemovePlBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            if (currentMenuSong && currentListMode === 'playlist_songs' && currentPlaylistId) {
                const pl = playlists.find(p => p.id === currentPlaylistId);
                if (pl) {
                    pl.song_ids = pl.song_ids || [];
                    pl.song_ids = pl.song_ids.filter(id => id !== currentMenuSong.id);
                    savePlaylistsAPI();
                    
                    if (pl.is_fav) {
                        let currentPlayingSong = playingFromUserQueue ? userQueue[userQueueIndex] : baseQueue[baseIndex];
                        if (currentPlayingSong && currentPlayingSong.id === currentMenuSong.id) {
                            heartBtn.classList.remove('heart-active');
                        }
                    }
                    renderPlaylistSongs(currentPlaylistId);
                }
                globalSongMenu.classList.remove('show');
            }
        });

        gsmQueueBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            if (currentMenuSong) {
                if (currentListMode === 'queue') {
                    if (currentMenuIndex === userQueueIndex && playingFromUserQueue) {
                        alert("Cannot remove the currently playing song from the queue.");
                    } else {
                        userQueue.splice(currentMenuIndex, 1);
                        if (currentMenuIndex < userQueueIndex) userQueueIndex--;
                        
                        if (userQueue.length === 0) listDropdown.innerHTML = '<li style="text-align:center; padding:20px;">Queue is empty</li>';
                        else renderList(userQueue);
                    }
                } else {
                    const idx = userQueue.findIndex(q => q.id === currentMenuSong.id);
                    if (idx > -1) {
                        if (idx === userQueueIndex && playingFromUserQueue) {
                            alert("Cannot remove the currently playing song from the queue.");
                        } else {
                            userQueue.splice(idx, 1);
                            if (idx < userQueueIndex) userQueueIndex--;
                        }
                    } else {
                        userQueue.push(currentMenuSong);
                    }
                }
                globalSongMenu.classList.remove('show');
            }
        });

        gsmDlBtn.addEventListener('click', async (e) => {
            e.stopPropagation();
            if (currentMenuSong) {
                gsmDlBtn.innerText = 'Working...';
                await toggleDownloadStatus(currentMenuSong);
                await checkDownloadedStatus(currentMenuSong, gsmDlBtn);
                globalSongMenu.classList.remove('show');
            }
        });

        const artSizeSlider = document.getElementById('artSizeSlider');
        const titleSizeSlider = document.getElementById('titleSizeSlider');
        const artSizeVal = document.getElementById('artSizeVal');
        const titleSizeVal = document.getElementById('titleSizeVal');
        const accentInput = document.getElementById('accentInput');
        const applyAccentBtn = document.getElementById('applyAccentBtn');
        const resetAccentBtn = document.getElementById('resetAccentBtn');
        const resetUIBtn = document.getElementById('resetUIBtn');

        let savedArtSize = localStorage.getItem('neoArtSize');
        let savedTitleSize = localStorage.getItem('neoTitleSize');
        let savedAccentColor = localStorage.getItem('neoAccentColor');

        if (savedAccentColor) { document.documentElement.style.setProperty('--accent', savedAccentColor); accentInput.value = savedAccentColor.replace(/^#/, ''); }
        if (savedArtSize) { document.documentElement.style.setProperty('--art-size', savedArtSize + 'px'); artSizeSlider.value = savedArtSize; artSizeVal.innerText = savedArtSize + 'px'; }
        if (savedTitleSize) { document.documentElement.style.setProperty('--title-size', savedTitleSize + 'px'); titleSizeSlider.value = savedTitleSize; titleSizeVal.innerText = savedTitleSize + 'px'; } 
        else if (window.innerWidth >= 768) { titleSizeSlider.value = 40; titleSizeVal.innerText = '40px'; }

        applyAccentBtn.addEventListener('click', () => {
            let colorVal = accentInput.value.trim().replace(/^#/, '');
            if (colorVal !== '') { let fullColor = '#' + colorVal; document.documentElement.style.setProperty('--accent', fullColor); localStorage.setItem('neoAccentColor', fullColor); accentInput.value = colorVal; }
        });
        resetAccentBtn.addEventListener('click', () => { document.documentElement.style.removeProperty('--accent'); localStorage.removeItem('neoAccentColor'); accentInput.value = ''; });
        
        artSizeSlider.addEventListener('input', (e) => { 
            let val = e.target.value;
            artSizeVal.innerText = val + 'px'; 
            document.documentElement.style.setProperty('--art-size', val + 'px'); 
            localStorage.setItem('neoArtSize', val);
        });
        titleSizeSlider.addEventListener('input', (e) => { 
            let val = e.target.value;
            titleSizeVal.innerText = val + 'px'; 
            document.documentElement.style.setProperty('--title-size', val + 'px'); 
            localStorage.setItem('neoTitleSize', val);
        });
        resetUIBtn.addEventListener('click', () => {
            localStorage.removeItem('neoArtSize'); localStorage.removeItem('neoTitleSize'); localStorage.removeItem('neoAccentColor');
            document.documentElement.style.removeProperty('--art-size'); document.documentElement.style.removeProperty('--title-size'); document.documentElement.style.removeProperty('--accent');
            accentInput.value = ''; artSizeSlider.value = 450; artSizeVal.innerText = '450px';
            let defaultTitleSize = window.innerWidth >= 768 ? 40 : 26; titleSizeSlider.value = defaultTitleSize; titleSizeVal.innerText = defaultTitleSize + 'px';
        });

        const settingsBtn = document.getElementById('settingsBtn');
        const settingsModal = document.getElementById('settingsModal');
        const closeSettings = document.getElementById('closeSettings');
        const saveSettings = document.getElementById('saveSettings');
        const bindInput = document.getElementById('bindInput');
        const portInput = document.getElementById('portInput');
        const dirInput = document.getElementById('dirInput');
        const settingsMsg = document.getElementById('settingsMsg');
        const rescanBtn = document.getElementById('rescanBtn');
        const rescanMsg = document.getElementById('rescanMsg');

        settingsBtn.addEventListener('click', () => {
            fetch('/api/config').then(res => res.json()).then(data => {
                bindInput.value = data.binding; portInput.value = data.port; dirInput.value = data.music_dir;
                settingsModal.style.display = 'flex'; settingsMsg.style.display = 'none'; rescanMsg.style.display = 'none';
            }).catch(err => console.error("Could not fetch config", err));
        });

        closeSettings.addEventListener('click', () => { settingsModal.style.display = 'none'; });

        saveSettings.addEventListener('click', () => {
            const newConfig = { binding: bindInput.value, port: portInput.value, music_dir: dirInput.value };
            fetch('/api/config', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(newConfig) })
            .then(res => { 
                if(res.ok) { 
                    settingsMsg.style.display = 'block'; 
                    loadMusic(); 
                    setTimeout(() => { 
                        settingsModal.style.display = 'none'; 
                        location.reload(); 
                    }, 2000); 
                }
            });
        });

        rescanBtn.addEventListener('click', () => {
            rescanMsg.style.display = 'block'; rescanMsg.innerText = 'Rescanning...'; rescanMsg.style.color = 'var(--text-secondary)';
            fetch('/api/rescan').then(() => { rescanMsg.innerText = 'Scan Complete!'; rescanMsg.style.color = '#4caf50'; loadMusic(); setTimeout(() => rescanMsg.style.display = 'none', 3000); })
            .catch(() => { rescanMsg.innerText = 'Error rescanning.'; rescanMsg.style.color = '#f44336'; });
        });

        const audio = document.getElementById('audio');
        const playBtn = document.getElementById('playBtn');
        const prevBtn = document.getElementById('prevBtn');
        const nextBtn = document.getElementById('nextBtn');
        const shuffleBtn = document.getElementById('shuffleBtn');
        const repeatBtn = document.getElementById('repeatBtn');
        const repeatDot = document.getElementById('repeatDot');
        
        const titleEl = document.getElementById('title');
        const artistEl = document.getElementById('artist');
        const artContainer = document.getElementById('artContainer');
        const albumArt = document.getElementById('album-art');
        const bgBlur = document.getElementById('bgBlur');
        const currTimeEl = document.getElementById('currTime');
        const durTimeEl = document.getElementById('durTime');
        
        const searchInput = document.getElementById('searchInput');
        const playlistsBtn = document.getElementById('playlistsBtn');
        const queueBtn = document.getElementById('queueBtn');
        const libraryBtn = document.getElementById('libraryBtn');
        const listDropdown = document.getElementById('listDropdown');
        
        const lyricsBtn = document.getElementById('lyricsBtn');
        const lyricsBox = document.getElementById('lyricsBox');
        const heartBtn = document.getElementById('heartBtn');
        const downloadBtn = document.getElementById('downloadBtn');

        const volumeSlider = document.getElementById('volumeSlider');
        const muteBtn = document.getElementById('muteBtn');

        const visContainer = document.getElementById('visContainer');
        const visCanvas = document.getElementById('visCanvas');
        const canvasCtx = visCanvas.getContext('2d');
        
        let audioCtx, analyser, source, dataArray, bufferLength;
        let isAudioInit = false;

        let baseQueue = [];
        let baseIndex = 0;
        let userQueue = [];
        let userQueueIndex = -1;
        let playingFromUserQueue = false;

        let isPlaying = false;
        let isShuffle = false;
        let repeatMode = 0; 
        let currentListMode = ''; 
        let currentPlaylistId = null;

        function updatePositionState() {
            if ('mediaSession' in navigator && !isNaN(audio.duration) && audio.duration > 0) {
                try {
                    navigator.mediaSession.setPositionState({
                        duration: audio.duration, playbackRate: audio.playbackRate || 1, position: audio.currentTime
                    });
                } catch (e) {}
            }
        }

        if ('mediaSession' in navigator) {
            navigator.mediaSession.setActionHandler('play', playAudio);
            navigator.mediaSession.setActionHandler('pause', pauseAudio);
            navigator.mediaSession.setActionHandler('previoustrack', prevSong);
            navigator.mediaSession.setActionHandler('nexttrack', nextSong);
            navigator.mediaSession.setActionHandler('seekbackward', (details) => { audio.currentTime = Math.max(audio.currentTime - (details.seekOffset || 10), 0); updatePositionState(); });
            navigator.mediaSession.setActionHandler('seekforward', (details) => { audio.currentTime = Math.min(audio.currentTime + (details.seekOffset || 10), audio.duration); updatePositionState(); });
            navigator.mediaSession.setActionHandler('seekto', (details) => {
                if (details.fastSeek && 'fastSeek' in audio) audio.fastSeek(details.seekTime); else audio.currentTime = details.seekTime;
                updatePositionState();
            });
        }

        audio.addEventListener('loadedmetadata', updatePositionState);
        audio.addEventListener('play', updatePositionState);
        audio.addEventListener('pause', updatePositionState);

        let savedVolume = localStorage.getItem('neoVolume');
        if (savedVolume !== null) { savedVolume = parseFloat(savedVolume); audio.volume = savedVolume; volumeSlider.value = savedVolume; } 
        else { audio.volume = 1; volumeSlider.value = 1; }
        updateVolumeUI(volumeSlider.value);

        function updateVolumeUI(val) {
            const percent = val * 100;
            volumeSlider.style.background = "linear-gradient(to right, #ffffff " + percent + "%, rgba(255,255,255,0.3) " + percent + "%)";
            if (val == 0 || audio.muted) muteBtn.classList.add('muted'); else muteBtn.classList.remove('muted');
        }

        volumeSlider.addEventListener('input', (e) => { const val = e.target.value; audio.volume = val; audio.muted = false; updateVolumeUI(val); localStorage.setItem('neoVolume', val); });
        muteBtn.addEventListener('click', () => {
            audio.muted = !audio.muted;
            if (audio.muted) updateVolumeUI(0);
            else { updateVolumeUI(audio.volume); if(audio.volume == 0) { audio.volume = 0.5; volumeSlider.value = 0.5; updateVolumeUI(0.5); localStorage.setItem('neoVolume', 0.5); } }
        });

        function initAudioVisualizer() {
            if (isAudioInit) return;
            isAudioInit = true;
            audioCtx = new (window.AudioContext || window.webkitAudioContext)();
            analyser = audioCtx.createAnalyser();
            analyser.fftSize = 128; bufferLength = analyser.frequencyBinCount; dataArray = new Uint8Array(bufferLength);
            source = audioCtx.createMediaElementSource(audio);
            source.connect(analyser); analyser.connect(audioCtx.destination);
            drawVisualizer();
        }

        function drawVisualizer() {
            requestAnimationFrame(drawVisualizer);
            if (isAudioInit) analyser.getByteFrequencyData(dataArray);
            canvasCtx.clearRect(0, 0, visCanvas.width, visCanvas.height);
            const bars = bufferLength || 64; const barWidth = visCanvas.width / bars;
            const progressRatio = audio.duration ? (audio.currentTime / audio.duration) : 0;
            const progressX = progressRatio * visCanvas.width;
            for(let i = 0; i < bars; i++) {
                let freqVal = dataArray ? dataArray[i] : 0;
                let barHeight = (freqVal / 255) * visCanvas.height * 0.8; if (barHeight < 6) barHeight = 6; 
                const x = i * barWidth; const y = (visCanvas.height - barHeight) / 2;
                if (x + (barWidth / 2) < progressX) canvasCtx.fillStyle = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim() || '#2979ff';
                else canvasCtx.fillStyle = 'rgba(255, 255, 255, 0.3)';
                canvasCtx.beginPath(); canvasCtx.roundRect(x + 2, y, barWidth - 4, barHeight, 4); canvasCtx.fill();
            }
        }
        drawVisualizer();

        let isDraggingVis = false;
        function handleSeek(e) {
            if (!audio.duration) return;
            const rect = visContainer.getBoundingClientRect();
            let clientX = e.touches ? e.touches[0].clientX : e.clientX;
            let clickX = clientX - rect.left;
            if (clickX < 0) clickX = 0; if (clickX > rect.width) clickX = rect.width;
            audio.currentTime = (clickX / rect.width) * audio.duration;
            updatePositionState();
        }

        visContainer.addEventListener('mousedown', (e) => { isDraggingVis = true; handleSeek(e); });
        visContainer.addEventListener('touchstart', (e) => { isDraggingVis = true; handleSeek(e); });
        window.addEventListener('mouseup', () => isDraggingVis = false); window.addEventListener('touchend', () => isDraggingVis = false);
        window.addEventListener('mousemove', (e) => { if (isDraggingVis) handleSeek(e); }); window.addEventListener('touchmove', (e) => { if (isDraggingVis) handleSeek(e); });

        async function loadMusic() {
            try {
                const res = await fetch('/api/songs');
                songs = await res.json() || [];
                
                const pRes = await fetch('/api/playlists');
                let fetchedPlaylists = await pRes.json() || [];
                playlists = fetchedPlaylists.map(pl => {
                    if (!pl.song_ids) pl.song_ids = [];
                    return pl;
                });

                if (songs.length > 0) {
                    if (baseQueue.length === 0) baseQueue = [...songs];
                    
                    if (!audio.src || !isPlaying) loadSong(baseQueue[baseIndex]);
                    else if (listDropdown.style.display === 'block') {
                        if (currentListMode === 'library') renderList(songs);
                        else if (currentListMode === 'playlist_songs') renderPlaylistSongs(currentPlaylistId);
                        else if (currentListMode === 'queue') renderList(userQueue);
                        else if (currentListMode === 'playlists_root') renderPlaylistsRoot();
                    }
                } else {
                    titleEl.innerText = "No music found";
                    artistEl.innerText = "Library is empty";
                }
            } catch (err) {
                titleEl.innerText = "Offline Mode";
                artistEl.innerText = "Check downloads";
            }
        }

        async function checkDownloadedStatus(song, btnEl = null) {
            if (!('caches' in window)) return false;
            const cache = await caches.open('neomusic-v13');
            const res = await cache.match(song.audio_url);
            const isDownloaded = !!res;
            
            if (btnEl) btnEl.innerText = isDownloaded ? 'Remove Download' : 'Download';
            
            let currentPlayingSong = playingFromUserQueue ? userQueue[userQueueIndex] : baseQueue[baseIndex];
            if (currentPlayingSong && currentPlayingSong.id === song.id) {
                if (isDownloaded) downloadBtn.classList.add('downloaded');
                else downloadBtn.classList.remove('downloaded');
            }
            return isDownloaded;
        }

        async function toggleDownloadStatus(song) {
            if (!('caches' in window)) return;
            const cache = await caches.open('neomusic-v13');
            const res = await cache.match(song.audio_url);
            
            if (res) {
                await cache.delete(song.audio_url);
                if (song.art_url) await cache.delete(song.art_url);
            } else {
                try {
                    await cache.add(song.audio_url);
                    if (song.art_url) await cache.add(song.art_url);
                } catch (e) {}
            }
            await checkDownloadedStatus(song); 
        }

        function loadSong(song) {
            if (!song) return;
            titleEl.innerText = song.title;
            artistEl.innerText = song.artist;
            audio.src = song.audio_url;

            let favPl = playlists.find(p => p.is_fav);
            if (favPl) {
                favPl.song_ids = favPl.song_ids || [];
                if (favPl.song_ids.includes(song.id)) heartBtn.classList.add('heart-active');
                else heartBtn.classList.remove('heart-active');
            }

            checkDownloadedStatus(song);

            if (song.art_url) {
                albumArt.src = song.art_url; albumArt.style.display = 'block'; bgBlur.style.backgroundImage = 'url(' + song.art_url + ')';
            } else {
                albumArt.src = ''; albumArt.style.display = 'none'; bgBlur.style.backgroundImage = 'none';
            }

            if ('mediaSession' in navigator) {
                navigator.mediaSession.metadata = new MediaMetadata({
                    title: song.title, artist: song.artist, album: song.album || 'Unknown Album',
                    artwork: [{ src: song.art_url || '/icon.png', sizes: '512x512', type: 'image/png' }]
                });
            }

            if (song.lyrics && song.lyrics.trim() !== "") lyricsBox.innerHTML = song.lyrics.replace(/\n/g, '<br>');
            else lyricsBox.innerHTML = "<em>No lyrics available</em>";
            lyricsBox.scrollTop = 0;
            
            if (listDropdown.style.display === 'block') {
                if (currentListMode === 'library') renderList(songs);
                else if (currentListMode === 'playlist_songs') renderPlaylistSongs(currentPlaylistId);
                else if (currentListMode === 'search') triggerSearch(searchInput.value);
                else if (currentListMode === 'queue') renderList(userQueue);
            }
        }

        downloadBtn.addEventListener('click', () => {
            const song = playingFromUserQueue ? userQueue[userQueueIndex] : baseQueue[baseIndex];
            if (song) toggleDownloadStatus(song);
        });

        heartBtn.addEventListener('click', () => {
            const song = playingFromUserQueue ? userQueue[userQueueIndex] : baseQueue[baseIndex];
            if (song) {
                let favPl = playlists.find(p => p.is_fav);
                if (!favPl) return;
                
                favPl.song_ids = favPl.song_ids || [];
                if (favPl.song_ids.includes(song.id)) {
                    favPl.song_ids = favPl.song_ids.filter(id => id !== song.id);
                } else {
                    favPl.song_ids.push(song.id);
                }
                savePlaylistsAPI();
                
                if (favPl.song_ids.includes(song.id)) heartBtn.classList.add('heart-active');
                else heartBtn.classList.remove('heart-active');
                
                if (currentListMode === 'playlist_songs' && currentPlaylistId === favPl.id) {
                    renderPlaylistSongs(favPl.id);
                }
            }
        });

        function playAudio() {
            initAudioVisualizer();
            if (audioCtx && audioCtx.state === 'suspended') audioCtx.resume();
            audio.play().catch(e => console.log("Playback interrupted"));
            playBtn.classList.add('playing'); isPlaying = true;
        }
        function pauseAudio() { audio.pause(); playBtn.classList.remove('playing'); isPlaying = false; }
        function togglePlay() { if (isPlaying) pauseAudio(); else playAudio(); }

        function prevSong() {
            if (audio.currentTime > 3) { audio.currentTime = 0; return; }
            if (playingFromUserQueue) {
                if (userQueueIndex > 0) {
                    userQueueIndex--; loadSong(userQueue[userQueueIndex]); if (isPlaying) playAudio();
                } else { audio.currentTime = 0; }
                return;
            }
            baseIndex--; if (baseIndex < 0) baseIndex = baseQueue.length - 1;
            loadSong(baseQueue[baseIndex]); if (isPlaying) playAudio();
        }

        function nextSong() {
            if (userQueue.length > 0 && userQueueIndex < userQueue.length - 1) {
                userQueueIndex++; playingFromUserQueue = true; loadSong(userQueue[userQueueIndex]); if (isPlaying) playAudio(); return;
            }
            if (userQueue.length > 0 && userQueueIndex === userQueue.length - 1 && playingFromUserQueue) {
                if (repeatMode === 1) { userQueueIndex = 0; loadSong(userQueue[userQueueIndex]); if (isPlaying) playAudio(); } 
                else { pauseAudio(); userQueueIndex = 0; loadSong(userQueue[userQueueIndex]); }
                return;
            }
            if (!playingFromUserQueue && baseQueue.length > 0) {
                baseIndex++;
                if (baseIndex > baseQueue.length - 1) {
                    if (repeatMode === 1) baseIndex = 0;
                    else { baseIndex = baseQueue.length - 1; if (isPlaying) togglePlay(); return; }
                }
                loadSong(baseQueue[baseIndex]); if (isPlaying) playAudio();
            }
        }

        lyricsBtn.addEventListener('click', () => {
            artContainer.classList.toggle('show-lyrics');
            if (artContainer.classList.contains('show-lyrics')) { lyricsBtn.innerText = 'Hide Lyrics'; lyricsBtn.classList.add('active'); } 
            else { lyricsBtn.innerText = 'Show Lyrics'; lyricsBtn.classList.remove('active'); }
        });

        playBtn.addEventListener('click', togglePlay); prevBtn.addEventListener('click', prevSong); nextBtn.addEventListener('click', nextSong);

        repeatBtn.addEventListener('click', () => {
            repeatMode = (repeatMode + 1) % 3; repeatBtn.classList.remove('active', 'repeat-one'); audio.loop = false;
            if (repeatMode === 0) { repeatBtn.setAttribute('aria-label', 'Repeat is off'); repeatBtn.title = 'Repeat: OFF'; } 
            else if (repeatMode === 1) { repeatBtn.classList.add('active'); repeatBtn.setAttribute('aria-label', 'Repeat all songs'); repeatBtn.title = 'Repeat: ALL'; } 
            else if (repeatMode === 2) { repeatBtn.classList.add('active', 'repeat-one'); repeatBtn.setAttribute('aria-label', 'Repeat current song'); repeatBtn.title = 'Repeat: ONE'; audio.loop = true; }
            if (navigator.vibrate) navigator.vibrate(10);
        });

        shuffleBtn.addEventListener('click', () => {
            isShuffle = !isShuffle; let currentSong = baseQueue[baseIndex];
            if (isShuffle) {
                shuffleBtn.classList.add('active-state'); baseQueue = [...baseQueue].sort(() => Math.random() - 0.5);
            } else {
                shuffleBtn.classList.remove('active-state');
                if (currentListMode === 'playlist_songs') {
                    const pl = playlists.find(p => p.id === currentPlaylistId);
                    if (pl) {
                        pl.song_ids = pl.song_ids || [];
                        baseQueue = songs.filter(s => pl.song_ids.includes(s.id));
                    }
                } else {
                    baseQueue = [...songs];
                }
            }
            if (currentSong) baseIndex = baseQueue.findIndex(s => s.id === currentSong.id);
        });

        audio.addEventListener('timeupdate', () => {
            if (!audio.duration) return;
            let currMins = Math.floor(audio.currentTime / 60); let currSecs = Math.floor(audio.currentTime % 60); currTimeEl.innerText = currMins + ":" + (currSecs < 10 ? '0' : '') + currSecs;
            let durMins = Math.floor(audio.duration / 60); let durSecs = Math.floor(audio.duration % 60); durTimeEl.innerText = durMins + ":" + (durSecs < 10 ? '0' : '') + durSecs;
        });

        audio.addEventListener('ended', () => { if (repeatMode !== 2) nextSong(); });

        function renderPlaylistsRoot() {
            listDropdown.innerHTML = '';
            
            playlists.forEach(pl => {
                const li = document.createElement('li');
                
                const nameDiv = document.createElement('div');
                nameDiv.className = 'pl-hub-name';
                nameDiv.innerHTML = (pl.is_fav ? heartSvgIcon : folderSvgIcon) + '<span>' + pl.name + '</span>';
                
                li.appendChild(nameDiv);

                if (!pl.is_fav) {
                    const actionsDiv = document.createElement('div');
                    actionsDiv.className = 'pl-hub-actions';
                    
                    const editBtn = document.createElement('button');
                    editBtn.className = 'pl-hub-btn';
                    editBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path></svg>';
                    editBtn.onclick = (e) => { e.stopPropagation(); showEditPlaylistModal(pl.id); };
                    
                    const delBtn = document.createElement('button');
                    delBtn.className = 'pl-hub-btn';
                    delBtn.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"></polyline><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path></svg>';
                    delBtn.onclick = (e) => deletePlaylist(pl.id, e);
                    
                    actionsDiv.appendChild(editBtn);
                    actionsDiv.appendChild(delBtn);
                    li.appendChild(actionsDiv);
                }

                li.onclick = (e) => {
                    e.stopPropagation();
                    currentPlaylistId = pl.id;
                    currentListMode = 'playlist_songs';
                    renderPlaylistSongs(pl.id);
                };
                
                listDropdown.appendChild(li);
            });
            
            const addLi = document.createElement('li');
            addLi.innerHTML = '<span class="pl-hub-name" style="color:var(--accent); justify-content:center;">+ Create New Playlist</span>';
            addLi.onclick = (e) => { e.stopPropagation(); showEditPlaylistModal(); };
            listDropdown.appendChild(addLi);
            
            listDropdown.style.display = 'block';
        }

        function renderPlaylistSongs(plId) {
            const pl = playlists.find(p => p.id === plId);
            if (!pl) return;
            const ids = pl.song_ids || [];
            const songsInPl = songs.filter(s => ids.includes(s.id));
            renderList(songsInPl, { prependBack: true, playlistTitle: pl.name });
        }

        function renderList(listToRender, options = {}) {
            listDropdown.innerHTML = '';
            
            if (options.prependBack) {
                const backLi = document.createElement('li');
                backLi.innerHTML = '<span class="pl-hub-name" style="color:var(--text-secondary);">⬅ Back to Playlists</span>';
                backLi.style.background = 'rgba(0,0,0,0.4)';
                backLi.onclick = (e) => {
                    e.stopPropagation();
                    currentListMode = 'playlists_root';
                    renderPlaylistsRoot();
                };
                listDropdown.appendChild(backLi);
            }

            if (listToRender.length === 0) {
                const emptyLi = document.createElement('li');
                emptyLi.style.justifyContent = 'center';
                emptyLi.style.padding = '20px';
                emptyLi.innerText = "Empty";
                listDropdown.appendChild(emptyLi);
                listDropdown.style.display = 'block';
                return;
            }

            let activeSongId = null;
            if (playingFromUserQueue && userQueue[userQueueIndex]) activeSongId = userQueue[userQueueIndex].id;
            else if (!playingFromUserQueue && baseQueue[baseIndex]) activeSongId = baseQueue[baseIndex].id;

            listToRender.forEach((song, i) => {
                const li = document.createElement('li');
                
                if (currentListMode === 'queue') {
                    if (playingFromUserQueue && i === userQueueIndex) li.classList.add('active-song');
                    if (i < userQueueIndex) li.style.opacity = '0.4'; 
                } else {
                    if (!playingFromUserQueue && song.id === activeSongId) li.classList.add('active-song');
                }
                
                const optContainer = document.createElement('div');
                optContainer.className = 'song-options-container';

                const optBtn = document.createElement('button');
                optBtn.className = 'song-options-btn';
                optBtn.innerHTML = '<svg viewBox="0 0 24 24" width="20" height="20" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="1.5"></circle><circle cx="12" cy="5" r="1.5"></circle><circle cx="12" cy="19" r="1.5"></circle></svg>';

                optContainer.appendChild(optBtn);

                const infoDiv = document.createElement('div');
                infoDiv.className = 'song-info';
                infoDiv.innerHTML = '<span class="res-title">' + song.title + '</span><span class="res-artist">' + song.artist + '</span>';
                
                li.appendChild(optContainer);
                li.appendChild(infoDiv);

                optBtn.addEventListener('click', (e) => {
                    e.stopPropagation();
                    currentMenuSong = song;
                    currentMenuIndex = i;
                    
                    if (currentListMode === 'playlist_songs') {
                        gsmPlaylistBtn.innerText = 'Add to another Playlist...';
                        gsmRemovePlBtn.style.display = 'block';
                        
                        const pl = playlists.find(p => p.id === currentPlaylistId);
                        if (pl && pl.is_fav) {
                            gsmRemovePlBtn.innerText = 'Remove from Favorites';
                        } else {
                            gsmRemovePlBtn.innerText = 'Remove from Playlist';
                        }
                    } else {
                        gsmPlaylistBtn.innerText = 'Manage Playlists';
                        gsmRemovePlBtn.style.display = 'none';
                    }

                    if (currentListMode === 'queue') gsmQueueBtn.innerText = 'Remove from Queue';
                    else {
                        const inQ = userQueue.some(q => q.id === song.id);
                        gsmQueueBtn.innerText = inQ ? 'Remove from Queue' : 'Add to Queue';
                    }
                    
                    gsmDlBtn.innerText = 'Checking...';
                    checkDownloadedStatus(song, gsmDlBtn);

                    const rect = optBtn.getBoundingClientRect();
                    let topPos = rect.bottom + 5;
                    if (topPos + 140 > window.innerHeight) topPos = rect.top - 140 - 5;
                    
                    globalSongMenu.style.top = topPos + 'px';
                    globalSongMenu.style.left = rect.left + 'px';
                    globalSongMenu.classList.add('show');
                });

                li.addEventListener('click', (e) => {
                    if(e.target.closest('.song-options-container')) return;

                    if (currentListMode === 'queue') {
                        userQueueIndex = i;
                        playingFromUserQueue = true;
                        loadSong(userQueue[userQueueIndex]);
                    } else {
                        userQueue = []; userQueueIndex = -1; playingFromUserQueue = false;

                        if (currentListMode === 'search') baseQueue = [...songs];
                        else baseQueue = [...listToRender];
                        
                        if (isShuffle) baseQueue = [...baseQueue].sort(() => Math.random() - 0.5);

                        baseIndex = baseQueue.findIndex(q => q.id === song.id);
                        if (baseIndex === -1) baseIndex = 0;

                        loadSong(baseQueue[baseIndex]);
                    }

                    playAudio();
                    searchInput.value = '';
                    listDropdown.style.display = 'none';
                    globalSongMenu.classList.remove('show');
                });

                listDropdown.appendChild(li);
            });
            listDropdown.style.display = 'block';
        }

        playlistsBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            if (listDropdown.style.display === 'block' && (currentListMode === 'playlists_root' || currentListMode === 'playlist_songs')) {
                listDropdown.style.display = 'none';
                currentListMode = '';
                playlistsBtn.classList.remove('playlist-active');
            } else {
                searchInput.value = '';
                currentListMode = 'playlists_root';
                playlistsBtn.classList.add('playlist-active');
                queueBtn.classList.remove('playlist-active');
                libraryBtn.classList.remove('playlist-active');
                renderPlaylistsRoot();
            }
        });

        queueBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            if (listDropdown.style.display === 'block' && currentListMode === 'queue') {
                listDropdown.style.display = 'none';
                currentListMode = '';
                queueBtn.classList.remove('playlist-active');
            } else {
                searchInput.value = '';
                currentListMode = 'queue';
                queueBtn.classList.add('playlist-active');
                playlistsBtn.classList.remove('playlist-active');
                libraryBtn.classList.remove('playlist-active');
                
                if (userQueue.length === 0) {
                    listDropdown.innerHTML = '<li style="text-align:center; padding:20px;">Queue is empty</li>';
                    listDropdown.style.display = 'block';
                } else renderList(userQueue);
            }
        });

        libraryBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            if (listDropdown.style.display === 'block' && currentListMode === 'library') {
                listDropdown.style.display = 'none';
                currentListMode = '';
                libraryBtn.classList.remove('playlist-active');
            } else {
                searchInput.value = '';
                currentListMode = 'library';
                libraryBtn.classList.add('playlist-active');
                playlistsBtn.classList.remove('playlist-active');
                queueBtn.classList.remove('playlist-active');
                renderList(songs);
            }
        });

        function triggerSearch(term) {
            term = term.toLowerCase().trim();
            if (!term) { listDropdown.style.display = 'none'; return; }
            const filtered = songs.filter(s => s.title.toLowerCase().includes(term) || s.artist.toLowerCase().includes(term));
            renderList(filtered);
        }

        searchInput.addEventListener('input', (e) => {
            currentListMode = 'search';
            playlistsBtn.classList.remove('playlist-active'); queueBtn.classList.remove('playlist-active'); libraryBtn.classList.remove('playlist-active');
            triggerSearch(e.target.value);
        });

        listDropdown.addEventListener('scroll', () => { globalSongMenu.classList.remove('show'); });

        document.addEventListener('click', (e) => {
            if (!e.target.closest('.top-bar') && !e.target.closest('.modal-bg')) {
                listDropdown.style.display = 'none';
                playlistsBtn.classList.remove('playlist-active'); queueBtn.classList.remove('playlist-active'); libraryBtn.classList.remove('playlist-active');
            }
            if (!e.target.closest('.song-options-btn') && !e.target.closest('#globalSongMenu')) {
                globalSongMenu.classList.remove('show');
            }
        });

        loadMusic();
    </script>
</body>
</html>`)
