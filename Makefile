.PHONY: lint-test

lint-test:
	go test . -run '^TestLintTestGoroutines' -count=1
