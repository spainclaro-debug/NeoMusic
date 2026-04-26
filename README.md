# NeoMusic

A neomorphic music player that runs on Termux and features a modern web interface powered by Go.

## Overview

NeoMusic is a fast and efficient music player with a modern, neomorphic UI design. This update transitions the server from Python to Go, resulting in significant performance improvements and cleaner code organization. The player now supports PWA (Progressive Web App) features and offline functionality, with a responsive design that works seamlessly on phones, tablets, and desktop browsers.

## What's New

- **Go Backend**: Server rewritten in Go for faster performance and efficient directory scanning
- **Dynamic Directory Scanning**: Automatically scans `/storage` directory instead of hardcoded paths
- **Modern UI**: Completely redesigned interface with a contemporary aesthetic, optimized for all devices
- **PWA Support**: Install as an app on your device
- **Offline Support**: Limited offline functionality for the host device
- **Improved Organization**: Cleaner and more organized file structure

## Installation

### Requirements
- Termux (Android)
- Go (Go 1.x or later)

### Steps

1. **Install Go**
   ```bash
   pkg install golang
   ```

2. **Download NeoMusic**
   - Go to the [Releases](../../releases) section
   - Download `NeoMusic.tar.gz`

3. **Extract the Archive**
   ```bash
   tar -xzvf NeoMusic.tar.gz
   ```

4. **Build the Application**
   ```bash
   go build -o neomusic builder.go
   ```
   Or with a custom name:
   ```bash
   go build -o [your-preferred-name] builder.go
   ```

5. **Run the Application**
   ```bash
   ./neomusic
   ```
   Or with your custom name:
   ```bash
   ./[your-preferred-name]
   ```

6. **Access the Player**
   - The server will display the port in your terminal (default: `localhost:1220`)
   - Open your browser and navigate to the displayed address
   - The initial startup may take a few seconds depending on your music library size, but is significantly faster than the Python version

## Features

- ⏯️ **Playback Control**: Play/Pause, Next, Previous
- 🔀 **Shuffle Mode**: Randomize playback order
- 🔁 **Repeat Modes**: Repeat all songs or repeat current song
- ❤️ **Favorites**: Mark and manage your favorite tracks
- 🔍 **Search**: Search songs by title or artist
- ⏱️ **Seek Control**: Drag the progress bar to seek forward or backward
- 🔊 **Volume Control**: Adjust volume (note: may have limited support on Apple devices due to permissions)
- 📱 **PWA Support**: Install as an app on your device
- 📁 **Configurable Path**: Change your music directory path from the UI
- ⚙️ **Port Configuration**: Customize the server port from the UI
- 📱 **Responsive Design**: Works perfectly on phones, tablets, and desktop browsers

## Configuration

After starting the application, you can configure:
- **Music Directory Path**: Change from the default `/storage` path
- **Server Port**: Modify the port if the default `1220` is already in use

## Known Limitations

- Volume control may not work on Apple devices due to platform-specific permission restrictions
- Offline support is limited to the host device only

## Contributing

Contributions are welcome! Feel free to fork the repository and submit pull requests.

## License

Please see the LICENSE file for more information.

---

Built with ❤️ for music lovers using Go and modern web technologies.
