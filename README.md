# ERC-4337 Bundler

A minimal ERC-4337 bundler written in Go. The service accepts UserOperations over JSON-RPC, validates them against the EntryPoint contract, keeps valid operations in an in-memory mempool, and periodically submits batches to Celo Sepolia.

## Current configuration

The executable is currently configured for:

- Network: Celo Sepolia
- Chain ID: `11142220`
- RPC: `https://forno.celo-sepolia.celo-testnet.org`
- EntryPoint v0.6: `0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789`
- HTTP endpoint: `http://localhost:4337/rpc`
- Maximum operations per bundle: `10`
- Bundle polling interval: `5` seconds

These values are defined in `cmd/api/main.go` and are not configurable yet.

## Requirements

- Go `1.25.3` or newer
- A funded EVM account on Celo Sepolia
- Access to the configured Celo Sepolia RPC endpoint

The bundler account pays gas for submitted bundles. Use a dedicated testnet key and never commit a private key to the repository.

## Run locally

Set the private key in the shell before starting the service. The application does not load `.env` files automatically.

```sh
export BUNDLER_PRIVATE_KEY=your_hex_private_key
go run ./cmd/api
```

The key may be provided with or without the `0x` prefix. On startup, the service listens on `:4337`.

To use the example environment file as a starting point:

```sh
cp .env.example .env
# Edit .env, then export the variable in your shell.
export BUNDLER_PRIVATE_KEY=your_hex_private_key
go run ./cmd/api
```

The `.env` file is only a template; it is not read by the application.

## JSON-RPC methods

Requests must be JSON-RPC 2.0 objects sent to `/rpc` with an HTTP `POST` request. Supported methods are:

| Method | Description |
| --- | --- |
| `eth_sendUserOperation` | Validates and adds a UserOperation to the mempool. Returns the UserOperation hash. |
| `eth_estimateUserOperationGas` | Estimates `preVerificationGas`, `verificationGasLimit`, and `callGasLimit`. |
| `eth_getUserOperationReceipt` | Returns the receipt for a mined UserOperation, or `null` while it is pending. |
| `eth_getTransactionReceipt` | Returns an underlying transaction receipt, or `null` if it is not available. |
| `eth_supportedEntryPoints` | Returns the configured EntryPoint address. |

### Check supported EntryPoints

```sh
curl -s http://localhost:4337/rpc \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_supportedEntryPoints","params":[]}'
```

### Estimate UserOperation gas

UserOperation byte fields and numeric fields use `0x`-prefixed hexadecimal JSON-RPC values.

```sh
curl -s http://localhost:4337/rpc \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc":"2.0",
    "id":1,
    "method":"eth_estimateUserOperationGas",
    "params":[{
      "sender":"0x0000000000000000000000000000000000000000",
      "nonce":"0x0",
      "initCode":"0x",
      "callData":"0x",
      "callGasLimit":"0x0",
      "verificationGasLimit":"0x0",
      "preVerificationGas":"0x0",
      "maxFeePerGas":"0x0",
      "maxPriorityFeePerGas":"0x0",
      "paymasterAndData":"0x",
      "signature":"0x"
    },"0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"]
  ]}'
```

For `eth_sendUserOperation`, the UserOperation is the first parameter. The second parameter should be the EntryPoint address.

## Development

Run the test suite with:

```sh
go test ./...
```

The Makefile contains a binding-generation target for `pkg/abi/EntryPoint.abi`:

```sh
make gen-abi
```

This requires `abigen` to be installed and available on `PATH`.

## Limitations

This is an early implementation intended for development and testnet experimentation. In particular:

- Configuration is hard-coded for Celo Sepolia.
- The mempool is in memory and is lost when the process stops.
- The embedded EntryPoint ABI is intentionally minimal.
- `preVerificationGas` is currently a fixed placeholder value of `21000`.
- Bundle gas uses conservative fixed overhead estimates rather than estimating the assembled `handleOps` transaction.
- The HTTP server has no authentication, TLS, request-size limit, or operational metrics.
- Validation and bundling behavior should be reviewed carefully before use with real funds.