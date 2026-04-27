# NeoMusic

A modern music player that runs on Termux and features a sleek modern UI powered by Go.

## Overview

NeoMusic is a fast and efficient music player with a modern, clean UI design. The application combines a powerful Go backend with a contemporary web interface for an exceptional listening experience.

## What's New

- **Modern UI Design**: Sleek and contemporary interface replacing the previous neomorphic design
- **Go Backend**: Server rewritten in Go for faster performance and efficient directory scanning
- **Dynamic Directory Scanning**: Automatically scans `/storage` directory instead of hardcoded paths
- **PWA Support**: Install as an app on your device
- **Offline Support**: Limited offline functionality for the host device
- **Improved Organization**: Cleaner and more organized file structure
- **New Shuffle and Repeat Button Designs**: Enhanced UI for playback controls
- **Playlist Support**: Create and manage custom playlists
- **Color Accent Support**: Customize the app's color theme
- **Font and Album Size Resizing**: Adjust interface elements to your preference
- **Install Script**: Automated installation process
- **Cleaned Up Files**: Removed unnecessary files for a leaner codebase

## Features

- **Playback Control**: Play/Pause, Next, Previous
- **Shuffle Mode**: Randomize playback order
- **Repeat Modes**: Repeat all songs or repeat current song
- **Playlist Support**: Create, manage, and organize custom playlists
- **Favorites**: Mark and manage your favorite tracks
- **Search**: Search songs by title or artist
- **Seek Control**: Drag the progress bar to seek forward or backward
- **Volume Control**: Adjust volume (note: may have limited support on Apple devices due to permissions)
- **Color Accent Support**: Customize the app's color theme
- **PWA Support**: Install as an app on your device
- **Configurable Path**: Change your music directory path from the UI
- **Port Configuration**: Customize the server port from the UI
- **Responsive Design**: Works perfectly on phones, tablets, and desktop browsers

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
