Neomusic player

A lightweight, self-hosted music player designed for Termux and mobile browsers. This player supports offline playback and high-performance local streaming using a Python-based server.
Installation and Setup

    Download the Core Files
    Place the following files into your desired project directory:

        index.html

        sw.js (Service Worker)

        manifest.json

        icon.png

    Prepare Your Music

        Create a folder named music in the same directory as your HTML file.

        Note: Ensure the folder name is all lowercase (music).

        Transfer your local .mp3 files into this folder.

        Optimization Tip: If you already have a large music library, it is faster to move the four core files listed above into your existing music folder rather than moving gigabytes of music into a new directory.

    File Structure
    For best results, keep all songs directly inside the music directory. Subfolders within the music directory have not been tested and may not be indexed by the player.

How to Run (Termux / Desktop)

    Open your terminal or Termux app.

    Navigate to your project directory.

    Ensure Python is installed.

    Start the local server by running:
    Bash

    python -m http.server 1234

    (You may replace 1234 with any available port).

    Open your web browser and visit: localhost:1234 or (your device ipaddress:1234) sample 192.168.x.x:1234 in your other device connected to the same network.
    
Customization

The player is built using standard HTML5 and CSS3. If you have experience with web design, you can easily modify the CSS within the index.html file to change the interface, colors, or layout to suit your preference.
