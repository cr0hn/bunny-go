# Contributing

Thanks for helping. Keep changes small and tested.

- Go 1.24+. Standard library only in the library packages.
- `make check` (lint + race tests + vet) must pass.
- Every exported identifier has a godoc comment; behaviour changes come with a test.
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/).

## Integration tests

`make test-integration` talks to the real bunny.net API and is opt-in (build
tag `integration`). It reads credentials from the environment
(`BUNNY_STORAGE_*`, `BUNNY_LIBRARY_ID`, `BUNNY_LIBRARY_API_KEY`). Tests must
only create and delete their own resources (files under
`_bunny-go-tests/<random>/`, videos in a collection the test creates). Never
list-and-delete, and never use a production account you are not willing to
touch.
