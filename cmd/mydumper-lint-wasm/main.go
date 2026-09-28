//go:build js && wasm

// Command mydumper-lint-wasm is mydumper-lint compiled to WebAssembly for the
// browser playground (web/playground). It exposes one global object,
// mydumperLint, whose functions return JSON strings:
//
//	mydumperLint.analyze(bytes: Uint8Array, name: string, version: string)
//	mydumperLint.versions()
//	mydumperLint.rules()
//	mydumperLint.build
//
// Everything runs in the page: the file never leaves the browser.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/tomsihap/mydumper-lint/internal/playground"
)

func main() {
	api := js.Global().Get("Object").New()
	api.Set("analyze", js.FuncOf(analyze))
	api.Set("versions", js.FuncOf(func(js.Value, []js.Value) any { return string(playground.Versions()) }))
	api.Set("rules", js.FuncOf(func(js.Value, []js.Value) any { return string(playground.Rules()) }))
	api.Set("build", playground.Build())
	js.Global().Set("mydumperLint", api)
	if js.Global().Get("dispatchEvent").Type() == js.TypeFunction { // a browser, not Node
		js.Global().Call("dispatchEvent", js.Global().Get("Event").New("mydumper-lint-ready"))
	}
	select {} // keep the functions alive
}

// analyze(bytes, name, version) returns the playground.Result as JSON.
func analyze(_ js.Value, args []js.Value) any {
	req := playground.Request{}
	if len(args) > 0 && args[0].Type() == js.TypeObject {
		req.Source = make([]byte, args[0].Get("length").Int())
		js.CopyBytesToGo(req.Source, args[0])
	}
	if len(args) > 1 && args[1].Type() == js.TypeString {
		req.Name = args[1].String()
	}
	if len(args) > 2 && args[2].Type() == js.TypeString {
		req.Version = args[2].String()
	}
	b, err := json.Marshal(playground.Analyze(req))
	if err != nil {
		b, _ = json.Marshal(map[string]string{"error": err.Error()})
	}
	return string(b)
}
