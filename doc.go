/*
Package ryegen allows the use of Go libraries from the Rye programming language (https://ryelang.org/).

It is an automatic binding generation utility that allows the creation of custom Rye interpreters, which include the functionality of specific Go libraries.

# Usage in a project

Create a folder called 'generate'.

Create a file called 'gen.go' with the following contents:

	package main

	import "github.com/refaktor/ryegen"

	//go:generate go run ./gen.go

	func main() {
		ryegen.Run()
	}

Run:

	go generate ./...

Edit the 'config.toml' file to your liking. See https://github.com/refaktor/rye-fyne/blob/main/generate/config.toml and https://github.com/refaktor/rye-ebitengine/blob/main/generate/config.toml for examples.

Optional: Edit 'bindings.txt' to exclude specific functions from your bindings.

Re-run:

	go generate ./...

...et voilà, you should have generated your binding files.

# Architecture pipeline (for developers)

Each element in the pipeline has distinct sub-packages that do a specific part. These are then "glued" together in the [Run] function.
 1. [config]: Parse user-supplied 'config.toml' and 'bindings.txt' files
 2. [repo] and [parser]: Fetch the target package and parse 'go.mod'/imports. Fetch dependencies recursively
 3. [ir]: Parse relevant package code AST and transform it into an intermediate representation
 3. [binder]: Use IR data to construct the final bindings. Save. Done.
*/
package ryegen