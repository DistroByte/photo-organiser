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
stack_binary = "~/personal/immich-stack/immich-stack"
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

### immich-stack

`photo-organiser stack` runs [immich-stack](https://github.com/Majorfi/immich-stack) with the configured server and API key, so stacking shares the same credentials as uploads. Every argument is passed through:

```
photo-organiser stack --dry-run
photo-organiser stack duplicates
photo-organiser stack fix-trash --trash-orphaned-raws
```

Set `stack_binary` to the binary's path if it is not on `PATH`.

#### Installing immich-stack

The simplest route is a [release archive](https://github.com/Majorfi/immich-stack/releases/latest), which contains a binary already named `immich-stack`:

```
curl -sL https://github.com/Majorfi/immich-stack/releases/latest/download/immich-stack_Darwin_arm64.tar.gz | tar -xz -C ~/.local/bin immich-stack
```

Swap `Darwin_arm64` for `Linux_x86_64`, `Linux_arm64` or `Darwin_x86_64` as needed.

Avoid `go install github.com/majorfi/immich-stack/cmd@latest`: it names the binary after its package directory, so you end up with a command called `cmd`. If you want to build from source instead, clone the repo and run `go build -o ~/.local/bin/immich-stack ./cmd`.

Whichever route you take, make sure the directory is on `PATH` or point `stack_binary` at the file.

## License

MIT
