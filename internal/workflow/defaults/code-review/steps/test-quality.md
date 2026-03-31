# Test Quality Review

Verify tests are actually testing something meaningful.

Coverage numbers lie. A test that can't fail provides no value.

Examine new and modified test files in the branch diff against main.

**Look for:**
- Weak assertions
  - Only checking err == nil without verifying the actual result
  - Only checking length > 0 without verifying contents
  - Using reflect.DeepEqual without understanding what's being compared

- Missing negative test cases
  - Happy path only, no error cases tested
  - No boundary testing (empty input, max values, nil)
  - No invalid input testing
  - No test for the "should fail" case

- Tests that can't fail
  - Mocked so heavily the test is meaningless
  - Testing implementation details rather than behavior
  - Assertions that are always true regardless of code changes
  - t.Log() without t.Error() or t.Fatal()

- Flaky test indicators
  - Sleep/delay in tests without synchronization
  - Time-dependent assertions (time.Now comparisons)
  - Reliance on map ordering
  - Port binding without dynamic allocation

- Missing test coverage for new code
  - New exported functions without any test
  - New error paths without negative tests
  - New branches (if/switch) without corresponding test cases

**Questions to answer:**
- Do these tests actually verify the behavior described in the writ?
- Would a bug in the implementation cause a test failure?
- Are edge cases and error paths tested?
- Would these tests catch a regression if someone changes this code later?
- Are test helpers and setup extracting common patterns?

Distinguish between missing tests (should add) and weak tests (should strengthen).
