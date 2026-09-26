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

# Count lines in Go files (regular vs comment lines), split into source and test files
wc:
    #!/usr/bin/env bash
    grand_regular=0
    grand_comment=0
    # report LABEL FILE... prints per-file counts and a subtotal for the group.
    report() {
        local label=$1; shift
        local group_regular=0 group_comment=0 regular comment total percent
        local -a lines=()
        for file in "$@"; do
            [ -f "$file" ] || continue
            read regular comment < <(awk '
                /^[[:space:]]*\/\// { comment++ }
                !/^[[:space:]]*\/\// { regular++ }
                END { printf "%d %d", regular, comment }
            ' "$file")
            total=$((regular + comment))
            percent=$(awk "BEGIN { printf \"%.1f\", ($comment / $total) * 100 }")
            lines+=("$(printf "%-8d %-8d %-8d %-8s %s" "$regular" "$comment" "$total" "$percent%" "$file")")
            group_regular=$((group_regular + regular))
            group_comment=$((group_comment + comment))
        done
        echo "== $label =="
        printf "%-8s %-8s %-8s %-8s %s\n" "regular" "comment" "total" "%//" "file"
        [ ${#lines[@]} -gt 0 ] && printf "%s\n" "${lines[@]}" | sort -n
        total=$((group_regular + group_comment))
        percent=$(awk "BEGIN { printf \"%.1f\", $total ? ($group_comment / $total) * 100 : 0 }")
        printf "%-8d %-8d %-8d %-8s %s\n\n" "$group_regular" "$group_comment" "$total" "$percent%" "subtotal"
        grand_regular=$((grand_regular + group_regular))
        grand_comment=$((grand_comment + group_comment))
    }
    shopt -s nullglob
    src=()
    for file in *.go; do
        [[ $file == *_test.go ]] || src+=("$file")
    done
    report "source" "${src[@]}"
    report "tests" *_test.go
    grand_total=$((grand_regular + grand_comment))
    total_percent=$(awk "BEGIN { printf \"%.1f\", ($grand_comment / $grand_total) * 100 }")
    printf "%-8d %-8d %-8d %-8s %s\n" "$grand_regular" "$grand_comment" "$grand_total" "$total_percent%" "total"
