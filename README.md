# Ryegen
Create a Rye interpreter with automatic bindings for a Go library.

## Create a binding (requires go1.24+)
### 1. Initialize a new module
- Create a new directory as you would for any new Go module.
- Open a command line inside the directory you just created.
- Initialize the Go module, e.g.: `go mod init my-module`

### 2. Install Ryegen as a tool
```
go get -tool github.com/refaktor/ryegen/v2@main
```

### 3. In that directory, create a `ryegen.toml`, e.g.
`ryegen.toml`:
```toml
# Use [[target]] to set options
# for a specific platform.
# the 'select' field accepts
# Go build constraints (see
# https://pkg.go.dev/cmd/go#hdr-Build_constraints).
# If you don't set select, all
# targets are selected.
# Set options in the last matching
# [[target]] block take precedence.
[[target]]
#select = 'windows && amd64' # uncomment to only apply the following options on windows amd64
cgo-enabled = false # set to true if the package needs cgo

# [[source]] specifies which
# Go code to generate bindings
# for. The 'packages' field specifies
# the base Go packages. Bindings
# will be generated for those
# packages, as well as any packages
# needed by their APIs.
[[source]]
# e.g. Go std net/http package, but this
# can also be online packages
packages = ['net/http']
```

### 4. Run Ryegen
```
go tool ryegen
```

- Tip: You can also use this with the [go:generate directive](https://go.dev/blog/generate).

- Tip: You can also cross-compile bindings easily, as long as they don't use CGo: `go tool ryegen -goos windows -goarch amd64`. See `go tool ryegen -h` for all options.

### 5. Run your Rye interpreter with bindings
`example.rye`:
```
http: import\go "net/http"

http/HandleFunc "/" fn { w r } {
    w .Write "Hello, world!"
}

port: ":8080"
print "listening on port " ++ port
http/ListenAndServe port nil
```

```
go run . ./example.rye
```

## Run an example
```
cd examples/fyne
go generate .
go run . ./example.rye
```

## Import dependency handling
Besides the selected packages, Ryegen will also generate bindings for any packages required by their public APIs.

Consider this example:

```go
func x() strings.Builder
func Y() bytes.Buffer
```

- Since `x` isn't exported, we don't generate bindings for it or the `strings` package.
- `Y`, however, is exported, so we *do* generate bindings for both `Y` and the `bytes` package. `bytes`, in turn, uses the `io` package in its API, meaning we also generate bindings for the `io` package.

## Note on channel garbage collection
TL;DR: At the moment, every converted channel that is left open will leak a fixed amount of memory - **close any channels once unused**.

Channels are converted by spawning a new goroutine which translates incoming values on the fly. At the moment, there is no known way to allow the garbage collector to clean up the goroutine's resources when the channel would have been GC'd (instead, the Rye and Go channels are just left dangling).

## Advanced: Debug info
### Profiling
Env: `RYEGEN_PROFILE=1`

Outputs a profile of the tool run to `ryegen_cpu.prof`.

You can use [pprof](https://github.com/google/pprof) to visualize it.

### Converter dependencies
Env: `RYEGEN_CONV_GRAPH=<regex>`

Outputs the converter dependency graph to `ryegen_conv_graph.gv`.

The value of the environment variable is a regex that matches methods or types in the converter graph. All nodes that match the regex and their dependencies will be shown.

Ryegen will use the `dot` command ([graphviz](https://graphviz.org/)), if it is in your system path.

### Imports
Env: `RYEGEN_IMPORT_GRAPH=<regex>`

Outputs the import dependency graph to `ryegen_import_graph.gv`.

The value of the environment variable is a regex that matches packages in the import graph. All imports that match the regex and their imports will be shown.

Ryegen will use the `dot` command ([graphviz](https://graphviz.org/)), if it is in your system path.