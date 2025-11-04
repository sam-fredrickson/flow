# Show available recipes
help:
    @just --list --unsorted

# Lint and format
lint:
    golangci-lint run --fix

# Run all tests
test:
    go test -v -count=1 ./...

# Run all tests with race detection
test-race:
    go test -v -count=1 -race ./...

# Run all tests & generate coverage report
test-cover:
     go test -coverprofile=coverage.out -coverpkg=.  ./...

# View current coverage report
view-coverage:
    go tool cover -func=coverage.out

# View current coverage report as HTML
view-coverage-html:
    go tool cover -html=coverage.out

# Run benchmarks
bench:
    go test -bench=. -benchmem ./bench/...

# Launch godoc web server
doc:
    go doc -all -http

# Count lines in Go files (regular vs comment lines)
wc:
    #!/usr/bin/env bash
    printf "%-8s %-8s %-8s %-8s %s\n" "regular" "comment" "total" "%//" "file"
    total_regular=0
    total_comment=0
    declare -a lines
    for file in *.go; do
        [ -f "$file" ] || continue
        read regular comment < <(awk '
            /^[[:space:]]*\/\// { comment++ }
            !/^[[:space:]]*\/\// { regular++ }
            END { printf "%d %d", regular, comment }
        ' "$file")
        total=$((regular + comment))
        percent=$(awk "BEGIN { printf \"%.1f\", ($comment / $total) * 100 }")
        lines+=("$(printf "%-8d %-8d %-8d %-8s %s" "$regular" "$comment" "$total" "$percent%" "$file")")
        total_regular=$((total_regular + regular))
        total_comment=$((total_comment + comment))
    done
    printf "%s\n" "${lines[@]}" | sort -n
    grand_total=$((total_regular + total_comment))
    total_percent=$(awk "BEGIN { printf \"%.1f\", ($total_comment / $grand_total) * 100 }")
    printf "%-8d %-8d %-8d %-8s %s\n" "$total_regular" "$total_comment" "$grand_total" "$total_percent%" "total"
