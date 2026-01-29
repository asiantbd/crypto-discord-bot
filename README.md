# crypto-discord-bot
[![CodeQL](https://github.com/asiantbd/crypto-discord-bot/actions/workflows/codeql-analysis.yml/badge.svg?branch=main)](https://github.com/asiantbd/crypto-discord-bot/actions/workflows/codeql-analysis.yml) [![Test](https://github.com/asiantbd/crypto-discord-bot/actions/workflows/testing.yml/badge.svg)](https://github.com/asiantbd/crypto-discord-bot/actions/workflows/testing.yml)

## Versioning

This project uses semantic versioning with automatic tagging on deployment. The version is tracked in the `VERSION` file at the root of the repository.

When code is deployed to production (pushed to the `main` branch), the CI/CD pipeline automatically creates a Git tag in the format `v<major>.<minor>.<patch>-<timestamp>` (e.g., `v1.0.0-20260129123456`).

To update the version, modify the `VERSION` file before merging to `main`.