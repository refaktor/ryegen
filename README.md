# ryegen

[![Go Reference](https://pkg.go.dev/badge/github.com/refaktor/ryegen.svg)](https://pkg.go.dev/github.com/refaktor/ryegen)

Package ryegen allows the use of Go libraries in the Rye programming language (https://ryelang.org/).

It is an automatic binding generation utility that allows the creation of custom Rye interpreters, which include the functionality of specific Go libraries.

## Getting started

Create a new Go project
```bash
mkdir my_ryegen_project
cd my_ryegen_project
go mod init my_ryegen_project
```

Set up ryegen using ryegen-init (replace "fyne.io/fyne/v2" with any Go library)
```bash
go run github.com/refaktor/ryegen/cmd/ryegen-init@latest fyne.io/fyne/v2
```

Run the generator
```bash
go mod tidy
go generate ./...
```

Edit the "config.toml" file to your liking. See https://github.com/refaktor/rye-fyne/blob/main/generate/config.toml and https://github.com/refaktor/rye-ebitengine/blob/main/generate/config.toml for examples.

Optional: Edit "bindings.txt" to exclude specific functions from your bindings.

Re-run `go generate ./...` after making any configuration changes.

Compile the Rye interpreter with bindings (replace "b_fyne" with "b_*<your Go library's module name>*")

	go build -tags b_fyne