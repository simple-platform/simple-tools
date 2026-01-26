#!/usr/bin/env node

const { execFile, spawn } = require('node:child_process')
const fs = require('node:fs')
const path = require('node:path')
const Stream = require('node:stream')
const util = require('node:util')
const axios = require('axios')

const pipeline = util.promisify(Stream.pipeline)

// Configuration
const VERSION = require('../package.json').version
// Assuming standardized naming in releases: contextualizer-v0.1.0-darwin-arm64
// Adjust repo URL to your actual repo
const REPO_URL = 'https://github.com/simple-platform/simple-tools'
const BIN_NAME = 'contextualizer'

const platformMap = {
  darwin: 'darwin',
  linux: 'linux',
  win32: 'windows',
}

const archMap = {
  arm64: 'arm64',
  x64: 'amd64',
}

// Determine current OS/Arch
// eslint-disable-next-line node/prefer-global/process
const platform = platformMap[process.platform]
// eslint-disable-next-line node/prefer-global/process
const arch = archMap[process.arch]

if (!platform || !arch) {
  // eslint-disable-next-line node/prefer-global/process
  console.error(`Unsupported platform: ${process.platform}-${process.arch}`)
  // eslint-disable-next-line node/prefer-global/process
  process.exit(1)
}

const binaryName = platform === 'windows' ? `${BIN_NAME}.exe` : BIN_NAME
const binaryPath = path.join(__dirname, binaryName)

// Check if --install-only flag is passed
// eslint-disable-next-line node/prefer-global/process
const isInstallOnly = process.argv.includes('--install-only')

async function downloadBinary() {
  let assetName = ''
  if (platform === 'darwin') {
    assetName = arch === 'arm64' ? `${BIN_NAME}-macos-silicon` : `${BIN_NAME}-macos`
  }
  else if (platform === 'linux') {
    assetName = arch === 'arm64' ? `${BIN_NAME}-linux-arm64` : `${BIN_NAME}-linux`
  }
  else if (platform === 'windows') {
    assetName = `${BIN_NAME}-windows.exe`
  }
  // Construct release URL (adjust based on actual release asset naming/location)
  // For monorepos, this might be tricky if releases aren't tagged per tool.
  // For now assuming tag matches version.
  const url = `${REPO_URL}/releases/download/v${VERSION}-contextualizer/${assetName}`

  // eslint-disable-next-line no-console
  console.log(`Downloading ${binaryName} from ${url}...`)

  try {
    const response = await axios({
      method: 'GET',
      responseType: 'stream',
      url,
    })

    const writer = fs.createWriteStream(binaryPath)
    await pipeline(response.data, writer)

    // Make executable
    if (platform !== 'windows') {
      fs.chmodSync(binaryPath, 0o755)
    }
    // eslint-disable-next-line no-console
    console.log('Download complete.')
  }
  catch (error) {
    if (isInstallOnly) {
      console.warn(`Failed to download binary: ${error.message}. This is expected if the release does not exist yet.`)
    }
    else {
      console.error(`Failed to download binary: ${error.message}`)
      // eslint-disable-next-line node/prefer-global/process
      process.exit(1)
    }
  }
}

async function run() {
  let needsDownload = true

  if (fs.existsSync(binaryPath)) {
    try {
      // Check version silently
      const execFileP = util.promisify(execFile)
      const { stdout } = await execFileP(binaryPath, ['--version'])

      if (stdout.trim() === VERSION) {
        needsDownload = false
      }
    }
    catch {
      // Binary exists but might be corrupted or wrong architecture, so re-download
    }
  }

  if (needsDownload) {
    // Attempt download
    await downloadBinary()

    if (!fs.existsSync(binaryPath)) {
      if (isInstallOnly)
        return // Exit gracefully if install-only and failed
      console.error('Binary not found and download failed.')
      console.error('Please ensure you have internet access or build the binary manually.')
      // eslint-disable-next-line node/prefer-global/process
      process.exit(1)
    }
  }

  if (isInstallOnly)
    return

  // Spawn the binary
  // eslint-disable-next-line node/prefer-global/process
  const child = spawn(binaryPath, process.argv.slice(2), {
    stdio: 'inherit',
  })

  child.on('close', (code) => {
    // eslint-disable-next-line node/prefer-global/process
    process.exit(code)
  })

  child.on('error', (err) => {
    console.error('Failed to start process:', err)
    // eslint-disable-next-line node/prefer-global/process
    process.exit(1)
  })
}

run().catch((err) => {
  console.error(err)
  // eslint-disable-next-line node/prefer-global/process
  process.exit(1)
})
