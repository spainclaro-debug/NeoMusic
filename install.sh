#!/data/data/com.termux/files/usr/bin/bash
set -e  # Stop on any error

echo "Updating packages..."
pkg update -y

echo "Installing necessary packages (golang)..."
pkg install golang -y

# Initialize module (idempotent: does nothing if go.mod already exists)
if [ ! -f go.mod ]; then
    echo "Initializing Go module..."
    go mod init mymusicserver
fi

echo "Downloading dependencies..."
go get github.com/dhowden/tag

echo "Tidying up module files (go.mod and go.sum)..."
go mod tidy

echo "Building the server..."
go build -o neomusic builder.go

echo "Success!"
echo "Run the server using: ./neomusic"
