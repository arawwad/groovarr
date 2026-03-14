PLUGIN_NAME=groovarrsimilarity
WASM_FILE=plugin.wasm
PACKAGE_FILE=$(PLUGIN_NAME).ndp

.PHONY: build package clean

build:
	GOOS=wasip1 GOARCH=wasm go build -o $(WASM_FILE) ./...

package: build
	zip -j $(PACKAGE_FILE) manifest.json $(WASM_FILE)

clean:
	rm -f $(WASM_FILE) $(PACKAGE_FILE)
