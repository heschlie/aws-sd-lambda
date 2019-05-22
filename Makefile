SHELL := /bin/bash

.PHONY package:
package:
	go build -o build/plosAutoSd
	pushd build && \
	zip plosAutoSd.zip plosAutoSd && \
	popd

.POHNY clean:
clean:
	rm -rf build/