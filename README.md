# photo-organiser

[![Go Reference](https://pkg.go.dev/badge/github.com/DistroByte/photo-organiser.svg)](https://pkg.go.dev/github.com/DistroByte/photo-organiser)
[![Go Report Card](https://goreportcard.com/badge/github.com/DistroByte/photo-organiser)](https://goreportcard.com/report/github.com/DistroByte/photo-organiser)

Import photos from a camera card, grouped by the date they were taken, into Immich or a remote directory over rsync.

The tool recognises the card on its own: Sony (stills and video clips), DJI, Canon and the Kodak Charmera. Plug the card in, run `photo-organiser`, and it mounts the card, transfers each day's photos, offers to clear the card, and unmounts it.

## Installation

```
go install github.com/DistroByte/photo-organiser@latest
```

or build from source:

```
git clone https://github.com/DistroByte/photo-organiser.git
cd photo-organiser
go build -o photo-organiser
```

Later releases install themselves with `photo-organiser update`.

## Setup

Run `init` once per machine. It prompts for each setting and writes `~/.config/photo-organiser/config.toml`, readable only by you:

```
photo-organiser init
```

The file is flat TOML and can be edited by hand:

```toml
server = "https://immich.local/api"
key = "..."
library = ""
host = "nas"
user = "james"
remote_path = "/volume1/photos"
device = "/dev/sdd1"
directory = "/dev/camera"
mount_type = "exfat"
```

Set `server` and `key` to upload to Immich, or `host` and `remote_path` to rsync over SSH. If both are set, photos go to Immich. Sony video clips always go over rsync, since they carry XML sidecars Immich does not accept.

Flags override `IMMICH_SERVER` and `IMMICH_API_KEY`, which override the file. Re-run `init --force` to rewrite the file; existing values are offered as defaults.

## Usage

```
photo-organiser            # import whatever card is plugged in
photo-organiser -n         # dry run: show what would happen, change nothing
photo-organiser --camera sony   # skip detection
photo-organiser sync       # trigger an Immich library scan
```

Set `mount_type = ""` to skip mounting and point `directory` at a card that is already mounted.

### Flags

```
  -s, --source string        source directory, overriding the layout's default
      --camera string        card layout to assume instead of detecting it (sony, dji, canon, charmera)
      --config string        config file (default ~/.config/photo-organiser/config.toml)
      --device string        device to mount (default "/dev/sdd1")
      --directory string     mount point (default "/dev/camera")
      --mount-type string    filesystem type for mounting; empty skips mounting (default "exfat")
      --server string        immich api base url, e.g. https://immich.local/api (env: IMMICH_SERVER)
      --key string           immich api key (env: IMMICH_API_KEY)
      --library string       immich library to scan after uploading
      --host string          remote host for rsync
      --user string          remote user for rsync (default "$USER")
      --remote-path string   remote destination path for rsync
  -v, --verbose              enable debug logging
  -n, --dry-run              will not move files, copy them to the remote, or cleanup source directories
```

## License

MIT
