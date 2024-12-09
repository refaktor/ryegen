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
go get -u github.com/refaktor/ryegen@main
go get -u github.com/refaktor/rye@main
```

Set up ryegen using ryegen-init (replace "fyne" with a name for your library and "fyne.io/fyne/v2" with any Go package)
```bash
go run github.com/refaktor/ryegen/cmd/ryegen-init@main fyne fyne.io/fyne/v2
```

Run the generator
```bash
go mod tidy
go generate ./...
go mod tidy
```

Edit the "config.toml" file to your liking. See https://github.com/refaktor/rye-fyne/blob/main/generate/config.toml and https://github.com/refaktor/rye-ebitengine/blob/main/generate/config.toml for examples.

Optional: Edit "bindings.txt" to exclude specific functions from your bindings.

Re-run `go generate ./...` after making any configuration changes.

Build the Rye interpreter with bindings
```bash
go build
```

## Adding another binding / managing bindings with build tags

Re-run ryegen-init with another name and package
```bash
go run github.com/refaktor/ryegen/cmd/ryegen-init@main ebiten github.com/hajimehoshi/ebiten
```

Compile the Rye interpreter with bindings
```bash
# Bind both libraries
go build
# Build without fyne
go build -tags "b_no_fyne"
```
You can customize the bindings' build tag names in their respective `config.toml` files.

## Environment Options
### Output Statistics to Console

`RYEGEN_STATS=1 go generate ./...`

<details>
<summary>Click to expand example statistics</summary>

```
====== BEGIN RYEGEN STATS ======

==Binding stats==
Generated 121 generic interface implementations.
Number of generated builtins (excludes generic interface impls):
|      CATEGORY       | WRITTEN/TOTAL |
|---------------------|---------------|
| Functions           |    476/476    |
| Getters             |    311/311    |
| Global vars/consts  |    295/295    |
| Methods             |   1670/1670   |
| Setters             |    311/311    |
| Struct initializers |     41/41     |
| ==TOTAL==           |   3104/3104   |

==Timing stats==
Fetched/checked source repos in 396.8254ms.
Binding generation tasks (excludes fetching/checking source repos):
|          TASK           |    TIME    | TIME % |
|-------------------------|------------|--------|
| Parse                   | 222.659ms  |  18.93 |
| Generate bindings       | 154.2164ms |  13.11 |
| Read/Write bindings.txt |  5.219ms   |   0.44 |
| Write and format code   | 794.1461ms |  67.52 |
| ==TOTAL==               | 1.1762405s |    100 |

======  END RYEGEN STATS  ======
```

</details>