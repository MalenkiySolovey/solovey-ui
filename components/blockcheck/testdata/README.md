`catalog-v1.sqlite.gz` is an inert synthetic configuration archive from the
stock `database/backup.Export` at Product `0b8b5fcb04747615a594be36c11d4d6b04f920d8`,
with BlockCheck `v0.1.16` and the old setting-only catalog. Only BlockCheck was
installed in the fixture's temporary database. The seeded `youtube` group was
explicitly removed before export; the restore test checks that it stays absent.
