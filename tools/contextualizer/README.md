# Contextualizer

Effortlessly package your project's source code into a single, context-rich file for LLMs. This CLI tool intelligently scans your directories, filters out unnecessary files, and consolidates the relevant code, making it easy to share your project's context with AI models.

![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)

## Features

- 🚀 **Go-based**: Fast, native performance with no runtime dependencies for consumers.
- 📦 **NPM Distribution**: Installs easily via `npm` or `npx` without requiring Go on the system.
- 🖥️ **Rich TUI**: Interactive directory selection using [Bubble Tea](https://github.com/charmbracelet/bubbletea).
- ⚙️ **Configurable**: robust ignore patterns and output customization via `contextualizer.json`.
- 🧠 **Smart Defaults**: Pre-configured to ignore common artifacts (`node_modules`, lockfiles, binaries).

## Installation

### Using NPM / NPX (Recommended)

You don't need Go installed. The package automatically downloads the correct binary for your OS/Arch.

```bash
# Run one-off
npx @simpleplatform/contextualizer

# Install globally
npm install -g @simpleplatform/contextualizer

# Install as dev dependency
npm install -D @simpleplatform/contextualizer
```

### Building from Source

If you prefer to build from source:

```bash
git clone https://github.com/simple-platform/simple-tools.git
cd simple-tools/tools/contextualizer
go build -o contextualizer ./cmd/contextualizer
```

## Usage

### 1. Initialize

Generate a `contextualizer.json` file in your project root with default ignore patterns:

```bash
contextualizer --init
```

### 2. Run

Simply run the command to start the interactive UI:

```bash
contextualizer
```

1.  **Select Directories**: Use `Space` to toggle directories to include.
2.  **Confirm**: Press `Enter` to proceed.
3.  **Output Mode**: Choose between `Multiple Files` (default) or `Single File`.

The tool will process the files and output them to `.context/`.

## Configuration

The `contextualizer.json` file allows for extensive customization:

```json
{
  "outputDir": ".context",
  "topLevelDirs": ["src", "pkg"],
  "ignore": [
    "node_modules/",
    "dist/",
    "*.log",
    "secret.txt"
  ]
}
```

## License

Apache 2.0
