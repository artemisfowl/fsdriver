# fsdriver
Web Driver like driver for streaming file data.

## Building and versioning `fsdriver`

`fsdriver` utilizes Git metadata combined with Go linker flags(`-ldflags`) to inject
versioning information directly into the binary at compile time. This ensures that
every binary built automatically carries its tag, commit hash and build timestamp
without needing manual version bumps in code.

### Build commands

| Command						  | Description																													|
| :---							  | ---:																																|
| `make`/`make build` | Builds the native binary for your host OS inside `./build/fsdriver` |
