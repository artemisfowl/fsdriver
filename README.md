# fsdriver
Web Driver like driver for streaming file data.

## Building and versioning `fsdriver`

`fsdriver` utilizes Git metadata combined with Go linker flags(`-ldflags`) to inject
versioning information directly into the binary at compile time. This ensures that
every binary built automatically carries its tag, commit hash and build timestamp
without needing manual version bumps in code.

### Build commands

| Command						  | Description																													 |
| :---							  | :---																																 |
| `make`/`make build` | Builds the native binary for your host OS inside `./build/fsdriver`  |
| `make all`				  | Cross-compiles binaries for Linux, macOS and Windows (amd64 & arm64) |
| `make version`		  | Prints the current Git version strings without building							 |
| `make clean`			  | Removes all the compiled artifacts in the `./build` directory				 |

### Checking the version of a compiled/downloaded binary

To verify the version information embedded inside any compiled/downloaded executable, run the binary
with the `--version` flag.

```bash
./build/fsdriver --version
fsdriver version v1.0.0 (commit: ef60be1, built at: 2026-10-18T07:51:00Z)
```

### Checking help information

To check the help information embedded inside the binary, run the binary with the `--help` flag.

```bash
./build/fsdriver --help
fsdriver - A Cross-platform, language agnostic file monitoring driver

Author: artemisfowl <skb655952@gmail.com>
License: GNU General Public License v3.0 (GPLv3)

Usage: ./fsdriver
  -version
        Print the version information and exit
```

### Tagging a new release

To release a new version, simply tag your commit in Git and run `make all`:

```bash
git tag -a v1.0.0 -m "Release v1.0.0"
make all
```

All generated binaries in `./build/` will automatically reflect the specified tag(in the aforementioned
example, it would be `v1.0.0`)
