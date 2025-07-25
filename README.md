# photo-organiser

Organise Sony camera photos into a date-based directory structure and sync them to a remote server.

## Installation

### From Source

```
git clone https://github.com/DistroByte/photo-organiser.git
cd photo-organiser
go build -o photo-organiser
```

### With Go Install

```
go install github.com/DistroByte/photo-organiser@latest
```

## Usage

### Basic Example

```
photo-organiser --mount-drive "f:" --mount-point /mnt/f --host your.remote.host --remote-path /remote/photos/path
```

- By default, the tool will use `<mount-point>/DCIM` as the source directory, in this case `/mnt/f/DCIM`.
- To specify a different source directory:

```
photo-organiser --mount-drive "f:" --mount-point /mnt/f --source /mnt/f/DCIM/10750715 --host your.remote.host --remote-path /remote/photos/path
```

### Flags

- `--mount-drive` (required): Drive to mount (e.g., `f:`)
- `--mount-point` (required): Mount point (e.g., `/mnt/f`)
- `--source`, `-s`: Source directory containing the photos (defaults to `/mount/point/DCIM`)
- `--host` (required): Remote host for `rsync`
- `--remote-path` (required): Remote destination path for `rsync`
- `--user`: Remote user for `rsync` (defaults to current user)
- `--dry-run`, `-n`: Preview actions without making changes
- `--verbose`, `-v`: Enable debug logging

### Example Full Command

```
photo-organiser --mount-drive "f:" --mount-point /mnt/f --host dionysus.internal --remote-path /volume1/homes/distro/Photos/Sony -v -n
```

## License

MIT
