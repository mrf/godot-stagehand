# Project-owned report card files

Everything in this directory is intentionally vendored into the repository:

- `config.toml` controls project copy, source location, gate, checks, and weights.
- `generate.py` runs the checks and emits the static `_site/` directory.
- `template.html` is the report page shell.
- `assets/` contains all browser CSS and JavaScript; nothing is fetched remotely.

Edit these files in the project that owns them. There is no central service to
configure and no runtime deployment to maintain.

