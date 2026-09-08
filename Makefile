# Variables
ABI_DIR  := pkg/abi
OUT_DIR  := pkg/entrypoint
ABI_FILE := $(ABI_DIR)/EntryPoint.abi
OUT_FILE := $(OUT_DIR)/entrypoint.go

.PHONY: all gen-abi clean

# Default target
all: gen-abi

# Target to generate Go contract bindings inside the pkg folder
gen-abi:
	@echo "Creating package directory: $(OUT_DIR)..."
	@mkdir -p $(OUT_DIR)
	@echo "Generating Go bindings for EntryPoint..."
	abigen --abi=$(ABI_FILE) --pkg=entrypoint --type=EntryPoint --out=$(OUT_FILE)
	@echo "Successfully generated $(OUT_FILE)"

# Remove generated artifacts
clean:
	@echo "Cleaning up generated package..."
	rm -rf $(OUT_DIR)
