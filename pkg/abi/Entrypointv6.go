package abi

// entryPointV06ABI is the ABI for the canonical v0.6 EntryPoint contract.
//
// TODO: this is currently a minimal placeholder covering only the methods
// and events this bundler actually calls (simulateValidation, handleOps,
// UserOperationEvent). Replace with the full official EntryPoint v0.6 ABI
// JSON before running against real UserOperations, or any additional
// method/event/error this code references later will fail to encode/decode.
const EntryPointV06ABI = `[
	{"type":"function","name":"simulateValidation","stateMutability":"nonpayable","inputs":[{"name":"userOp","type":"tuple","components":[
		{"name":"sender","type":"address"},
		{"name":"nonce","type":"uint256"},
		{"name":"initCode","type":"bytes"},
		{"name":"callData","type":"bytes"},
		{"name":"callGasLimit","type":"uint256"},
		{"name":"verificationGasLimit","type":"uint256"},
		{"name":"preVerificationGas","type":"uint256"},
		{"name":"maxFeePerGas","type":"uint256"},
		{"name":"maxPriorityFeePerGas","type":"uint256"},
		{"name":"paymasterAndData","type":"bytes"},
		{"name":"signature","type":"bytes"}
	]}],"outputs":[]},
	{"type":"function","name":"handleOps","stateMutability":"nonpayable","inputs":[
		{"name":"ops","type":"tuple[]","components":[
			{"name":"sender","type":"address"},
			{"name":"nonce","type":"uint256"},
			{"name":"initCode","type":"bytes"},
			{"name":"callData","type":"bytes"},
			{"name":"callGasLimit","type":"uint256"},
			{"name":"verificationGasLimit","type":"uint256"},
			{"name":"preVerificationGas","type":"uint256"},
			{"name":"maxFeePerGas","type":"uint256"},
			{"name":"maxPriorityFeePerGas","type":"uint256"},
			{"name":"paymasterAndData","type":"bytes"},
			{"name":"signature","type":"bytes"}
		]},
		{"name":"beneficiary","type":"address"}
	],"outputs":[]},
	{"type":"error","name":"ValidationResult","inputs":[
		{"name":"returnInfo","type":"tuple","components":[
			{"name":"preOpGas","type":"uint256"},
			{"name":"prefund","type":"uint256"},
			{"name":"sigFailed","type":"bool"},
			{"name":"validAfter","type":"uint48"},
			{"name":"validUntil","type":"uint48"},
			{"name":"paymasterContext","type":"bytes"}
		]},
		{"name":"senderInfo","type":"tuple","components":[{"name":"stake","type":"uint256"},{"name":"unstakeDelaySec","type":"uint256"}]},
		{"name":"factoryInfo","type":"tuple","components":[{"name":"stake","type":"uint256"},{"name":"unstakeDelaySec","type":"uint256"}]},
		{"name":"paymasterInfo","type":"tuple","components":[{"name":"stake","type":"uint256"},{"name":"unstakeDelaySec","type":"uint256"}]}
	]},
	{"type":"event","name":"UserOperationEvent","inputs":[
		{"name":"userOpHash","type":"bytes32","indexed":true},
		{"name":"sender","type":"address","indexed":true},
		{"name":"paymaster","type":"address","indexed":true},
		{"name":"nonce","type":"uint256","indexed":false},
		{"name":"success","type":"bool","indexed":false},
		{"name":"actualGasCost","type":"uint256","indexed":false},
		{"name":"actualGasUsed","type":"uint256","indexed":false}
	]}
]`
