#!/bin/bash

# Script to ingest RFCs and generate Go spec matrices
# Usage: ./ingest_rfcs.sh [RFC-NUMBER...]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$(dirname "$SCRIPT_DIR")")"
SPEC_DIR="$PROJECT_ROOT/spec"

# Default RFCs to ingest (core JMAP RFCs)
DEFAULT_RFCS=(
    "RFC8620"   # JMAP Core
    "RFC8621"   # JMAP Mail
    "RFC8887"   # JMAP WebSockets
    "RFC9007"   # JMAP MDN
    "RFC9219"   # JMAP S/MIME
    "RFC9404"   # JMAP Blob Management
    "RFC9425"   # JMAP Quotas
    "RFC9610"   # JMAP Contacts
    "RFC9661"   # JMAP Sieve
    "RFC9670"   # JMAP Sharing
    "RFC9698"   # JMAPACCESS
    "RFC9749"   # VAPID
)

# RFCs to process
RFCS=("${@:-${DEFAULT_RFCS[@]}}")

echo "Ingesting RFCs: ${RFCS[*]}"

# Build the specextract tool
echo "Building specextract tool..."
cd "$SCRIPT_DIR"
go build -o specextract .

# Process each RFC
echo "Processing RFCs..."
for rfc in "${RFCS[@]}"; do
    echo "Processing $rfc..."
    
    # Generate Go file for this RFC
    output_file="$SPEC_DIR/${rfc}_generated.go"
    
    # Run specextract to generate Go code
    ./specextract -format go -output "$output_file" "$rfc"
    
    if [ -f "$output_file" ]; then
        echo "Generated: $output_file"
    else
        echo "Failed to generate: $output_file"
    fi
done

echo "RFC ingestion complete!"

# Add a note about next steps
echo ""
echo "Next steps:"
echo "1. Review the generated files in $SPEC_DIR"
echo "2. Manually verify and clean up the extracted clauses"
echo "3. Add proper test references to each requirement"
echo "4. Update the status from Gap to Covered where appropriate"
echo "5. Register the new matrices in spec.Matrices"
echo "6. Run 'go generate ./spec' to update documentation"