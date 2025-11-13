# TODO

<https://github.com/moby/buildkit/issues/3037>

Exporter implementation does similar things to gatewayFrontend.Solve. Simpler
though, because we don't need to serve exporters an equivalent of Solve to make
recursive requests.

ATM we can just hijack the existing gateway implementation.

First:

- [x] Create a GatewayExporter that implements exporter.Exporter
- [x] Have it start a gateway container, and connect it to the gateway API
- [x] Have it operate on one of the inputs. "solve" it, and then use the resulting ref as the root for the exporter.
- [x] Allow passing options to the gateway exporter
- [x] Allow writing to a local file / directory
- [x] Add a mechanism for reading blobs
- [ ] Allow passing credentials

Next:

- [ ] Create a BuildFunc equivalent, but for exporters (to allow the client to do this)

Tidy-up:

- [ ] Optimize file reading into one mount
- [ ] Split out the gateway API into multiple separate APIs.
  - [ ] Common + frontend + exporter
- [ ] So much duplication between frontend and exporters...

---

Protobuf:

```proto
// similar to gateway, but the refs are immutable refs
message Result

// wraps an immutable ref (just the ID?)
message Ref

// can call ReadFile, ReadDir, StatFile
// can call Remote, which calls GetRemotes()[0], and returns a content store
// API wrapping the content.InfoReaderProvider
```

Steps:

- ...
- GatewayForwarder equivalent for client access

Problems:

- Ideally deduplicate between ReadFile/ReadDir/StatFile
  - Maybe also allow container api
- Allow wrapping ReadFile/ReadDir/StatFile with a single mount transaction
