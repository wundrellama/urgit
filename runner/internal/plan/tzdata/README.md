# Pinned timezone database

`zoneinfo.zip` is the IANA time zone database as packaged by the Go
toolchain: byte for byte `lib/time/zoneinfo.zip` of Go 1.27.1
(`go version go1.27.1 linux/amd64`), 408,467 bytes, SHA-256
`b2d18a7c8fa8142097a48c99609fb3c92db5ee98bc740294e57eab8ae9f94779`.
The workflow compiler (`plan.ZoneTable`) embeds it and reads every
`on.schedule[].timezone` from it alone, never from the host's database, the
`ZONEINFO` variable or the local zone (Delivery 1C).

`LICENSE` is the Go project's BSD-style license (SHA-256
`911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad`), under
which the Go authors distribute the packaged file. The time zone data itself
comes from the IANA tz database (https://www.iana.org/time-zones), whose
data is in the public domain.

A newer database is a new pinned copy: replace both files together, update
the SHA-256 in `zone_test.go`, and resolve and promote baselines again so
their catalogs carry offsets from it.
