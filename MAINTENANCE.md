# Dockertest v3 Maintenance Mode

## Status

Dockertest v3 is in **maintenance mode** as of 2026-01-24. All new development
happens in [dockertest v4](v4/).

## Support Policy

- **Security fixes**: Critical CVEs will be patched
- **Docker compatibility**: Updates for Docker API changes
- **Bug fixes**: Only critical issues affecting production users
- **New features**: None - use v4 for new functionality

## Support Timeline

- **Maintenance period**: 12 months from v4 GA release
- **End of support**: 2027-01-24
- **Migration deadline**: Recommended before end of support

## Migration

See [Migration Guide](UPGRADE.md) for detailed instructions on migrating to v4.

## Why v4?

- No vendored client (uses official github.com/moby/moby/client)
- Modern Go patterns (context, functional options, sentinel errors)
- 2-3x faster tests with automatic container reuse
- Better error handling and testing ergonomics
- Active development and feature additions

## Questions?

- v4 documentation: [v4/README.md](v4/README.md)
- Migration guide: [docs/migration-v3-to-v4.md](UPGRADE.md)
- Issues: [GitHub Issues](https://github.com/ory/dockertest/issues)
