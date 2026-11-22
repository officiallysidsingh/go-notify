# Contributing

Thanks for helping with **go-notify**. Please read the [Code of Conduct](CODE_OF_CONDUCT.md) first.

## Workflow

1. Fork and clone the repository, then create a branch from `main`.
2. Set up your tools and learn the layout in [docs/development.md](docs/development.md).
3. Make your change with tests. Statement coverage must stay at 100%.
4. Before opening a pull request, run:

   ```bash
   make lint
   make test
   make coverage   # needs Docker; fails below 100%
   ```

5. Open a pull request describing what changed and why. Use
   [Conventional Commits](https://www.conventionalcommits.org) style subjects
   (`feat:`, `fix:`, `docs:`, ...).

## Reporting issues

Open an issue with steps to reproduce, expected and actual behaviour, and the
Go version and OS you used. Do not include API keys or tokens.

## License

By contributing you agree your work is licensed under the [MIT License](LICENSE).
