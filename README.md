# photo-organiser

Organise camera photos into a date-based directories and sync them to a remote server with rsync.

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
photo-organiser sony --device /dev/sdd1 --host remote.host --user username --remote-path /path/on/remote
```

By default, the tool will use `<mount-point>/DCIM` as the source directory, in this case `/mnt/camera/DCIM`. To specify a different source directory:

```
photo-organiser sony --device /dev/sdd1 --directory /mnt/camera --source /mnt/camera/DCIM/10750715 --host remote.host --remote-path /remote/photos/path
```

### Flags

```
      --device string        device to mount (default "/dev/sdd1")
      --directory string     mount point (default "/dev/camera")
  -n, --dry-run              will not move files, copy them to the remote, or cleanup source directories
  -h, --help                 help for photo-organiser
      --host string          remote host for rsync
      --mount-type string    filesystem type for mounting (default "exfat")
      --remote-path string   remote destination path for rsync
      --source string        source directory containing the photos. (default /mount/point/DCIM)
      --user string          remote user for rsync (default "$USER")
  -v, --verbose              enable debug logging
```

### Example Full Command

```
photo-organiser sony --device /dev/sdd1 --directory /mnt/camera --host dionysus.internal --user distro --remote-path /volume1/homes/distro/Photos/Sony
```

## License

MIT
