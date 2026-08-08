# Protocol fixture provenance

Parser compatibility tests include complete H02 frames published by the
[Traccar project](https://github.com/traccar/traccar) in
`src/test/java/org/traccar/protocol/H02ProtocolDecoderTest.java` at commit
`17e7a330e8a07896f000898b37dc770f2df3c142`.

Traccar publishes those tests under the Apache License 2.0. The copied fixture
strings are annotated in `internal/protocol/sinotrack/location_test.go`. They
cover real protocol variations including omitted network fields, hexadecimal
network codes, extra trailing fields, whitespace, and a minutes-only longitude.

The fixtures are compatibility evidence for the H02 protocol family, not proof
that every frame originated from an ST-901. Captures from the target Herring
device and firmware remain required before declaring that model fully verified.

The parser layout was also compared with the MIT-licensed
[eusonlito/GPS-Tracker H02 parser](https://github.com/eusonlito/GPS-Tracker/tree/master/app/Services/Protocol/H02),
which documents the V1/V5/V6/V8 field order and acknowledgement shape. That
repository currently does not include equivalent raw-frame unit fixtures.
