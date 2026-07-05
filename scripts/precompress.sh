#!/bin/bash
# Pre-compress frontend assets for embedded serving
# Creates .gz variants alongside original files for zero-runtime CPU cost

set -e

DIST_DIR="${1:-frontend/dist}"

if [ ! -d "$DIST_DIR" ]; then
    echo "Error: $DIST_DIR does not exist"
    exit 1
fi

echo "Pre-compressing assets in $DIST_DIR..."

# Find all JS, CSS, HTML, and SVG files and compress them
find "$DIST_DIR" -type f \( -name "*.js" -o -name "*.css" -o -name "*.html" -o -name "*.svg" \) | while read -r file; do
    # Gzip compression (level 9 for maximum compression)
    gzip -9 -k -f "$file"
    echo "  gzip: ${file}.gz"

    # Zstd compression if available (level 19 for maximum)
    if command -v zstd &> /dev/null; then
        zstd -19 -q -f -o "${file}.zst" "$file"
        echo "  zstd: ${file}.zst"
    fi
done

echo ""
echo "Pre-compression complete!"

# Report compression summary
echo ""
echo "Compression summary:"

for ext in js css html; do
    # Count original files
    origFiles=$(find "$DIST_DIR" -name "*.$ext" -not -name "*.gz" -not -name "*.zst" 2>/dev/null | wc -l | tr -d ' ')

    if [ "$origFiles" -gt 0 ]; then
        # Calculate sizes
        origSize=$(find "$DIST_DIR" -name "*.$ext" -not -name "*.gz" -not -name "*.zst" -exec cat {} + 2>/dev/null | wc -c | tr -d ' ')
        gzSize=$(find "$DIST_DIR" -name "*.$ext.gz" -exec cat {} + 2>/dev/null | wc -c | tr -d ' ')

        if [ "$origSize" -gt 0 ] && [ "$gzSize" -gt 0 ]; then
            # Calculate reduction percentage
            reduction=$(echo "scale=1; 100 - ($gzSize * 100 / $origSize)" | bc 2>/dev/null || echo "N/A")
            echo "  .$ext: ${origSize} -> ${gzSize} bytes (${reduction}% reduction)"
        fi
    fi
done

echo ""
echo "Done!"
