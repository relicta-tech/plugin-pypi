# PyPI Plugin for Relicta

Official PyPI plugin for [Relicta](https://github.com/relicta-tech/relicta) - Publish packages to PyPI (Python Package Index).

## Installation

```bash
relicta plugin install pypi
relicta plugin enable pypi
```

## Configuration

Add to your `release.config.yaml`:

```yaml
plugins:
  - name: pypi
    enabled: true
    config:
      # Add configuration options here
```

## License

MIT License - see [LICENSE](LICENSE) for details.
