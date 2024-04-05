#
# Makefile
#

# ldflags variables to update --version
# short commit hash
COMMIT :=$(shell /usr/bin/git describe --always)
DATE :=$(shell /bin/date -u +"%Y-%m-%d-%H:%M")
BINARY :=routing

all: clean build version

test:
	go test

clean:
	[ -f ${BINARY} ] && /bin/rm -rf ./${BINARY} || true

build:
	CGO_ENABLED=0 go build -ldflags "-X main.commit=${COMMIT} -X main.date=${DATE}" -o ./${BINARY} ./${BINARY}.go

# https://words.filippo.io/shrink-your-go-binaries-with-this-one-weird-trick/
buildsmall:
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.commit=${COMMIT} -X main.date=${DATE}" -o ./${BINARY} ./${BINARY}.go

shrink:
	upx --brute ./${BINARY}

version:
	./${BINARY} --version

curl:
	curl --silent http://localhost:9901/metrics


example:
# fails because no header
	curl --silent -X POST http://localhost:2080/configure/stream1/source/group/port/blah.m3u8
# fails becasue incorrect key
	curl --silent -H "Magic-Key: unknown" -X POST http://localhost:2080/configure/stream1/source/group/port/blah.m3u8
# configures
	curl --silent -H "Magic-Key: abc123" -X POST http://localhost:2080/configure/stream1/source1/group1/port1/blah1.m3u8
	curl --silent -H "Magic-Key: abc123" -X POST http://localhost:2080/configure/stream2/source2/group2/port2/blah2.m3u8
	curl --silent -H "Magic-Key: abc123" -X POST http://localhost:2080/configure/stream3/source3/group3/port3/blah3.m3u8
	curl --silent -H "Magic-Key: abc123" -X POST http://localhost:2080/configure/stream4/source2/group2/port2/blah2.m3u8
	curl --silent -H "Magic-Key: abc123" -X POST http://localhost:2080/configure/stream5/source3/group3/port3/blah3.m3u8

# fails because no header
	curl --silent -X POST http://localhost:2080/delete/stream1/blah.m3u8
# fails becasue incorrect key
	curl --silent -H "Magic-Key: unknown" -X POST http://localhost:2080/delete/stream1/blah.m3u8
# configures
	curl --silent -H "Magic-Key: abc123" -X POST http://localhost:2080/delete/stream2/blah.m3u8
	curl --silent -H "Magic-Key: abc123" -X POST http://localhost:2080/delete/stream3/blah.m3u8
	curl --silent -H "Magic-Key: abc123" -X POST http://localhost:2080/delete/stream4/blah.m3u8

	curl --silent -X GET http://localhost:2080/stream/stream/blah1.m3u8
	curl --silent -X GET http://localhost:2080/stream/stream2/blah2.m3u8
	curl --silent -X GET http://localhost:2080/stream/stream3/blah3.m3u8

	curl --silent -X GET http://localhost:2080/stream/stream666/blah1.m3u8

	curl --silent -X GET http://localhost:2080/stream/stream1/blah666.m3u8
#