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
[[target]]
name = 'windows_amd64'
goos = 'windows'
goarch = 'amd64'
cgo-enabled = true

[[source]]
packages = ['github.com/go-p5/p5']
```

### 4. Run Ryegen
```
go tool ryegen
```

Re-run go generate any time you want to re-generate the bindings.

## Run an example
```
cd examples/fyne
go generate .
go run . ./example.rye
```

## Getting debug info (advanced)
### Profiling
Env: `RYEGEN_PROFILE=1`

Outputs a profile of the tool run to `ryegen_cpu.prof`.

You can use [pprof](https://github.com/google/pprof) to visualize it.

### Converter dependencies
Env: `RYEGEN_CONV_GRAPH=<regex>`

Outputs the converter dependency graph to `ryegen_conv_graph.gv`.

The value of the environment variable is a regex that matches methods or types in the converter graph. All nodes that match the regex and their dependencies will be shown.

You can use [graphviz](https://graphviz.org/)'s `dot` command to visualize the graph, for example:
```
dot -Tsvg ryegen_conv_graph.gv -o ryegen_conv_graph.svg
```