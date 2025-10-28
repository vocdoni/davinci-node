## Go Testing Guidelines

### QuickTest (qt) Library Best Practices

**Length Assertions**
- ✅ Use `c.Assert(s, qt.HasLen, N)` for string or slice length checks
- ❌ Avoid `c.Assert(len(s), qt.Equals, N)`

**Preferred Assertion Patterns:**
- ✅ `c.Assert(result, qt.DeepEquals, expected)` for struct/slice comparisons
- ✅ `c.Assert(value, qt.IsNil)` instead of `c.Assert(value, qt.Equals, nil)`
- ✅ `c.Assert(value, qt.Not(qt.IsNil))` instead of `c.Assert(value, qt.Not(qt.Equals), nil)`
- ✅ `c.Assert(boolean, qt.IsTrue)` instead of `c.Assert(boolean, qt.Equals, true)`
- ✅ `c.Assert(boolean, qt.IsFalse)` instead of `c.Assert(boolean, qt.Equals, false)`
- ✅ `c.Assert(string, qt.Contains, substring)` for substring checks
- ✅ `c.Assert(slice, qt.Contains, element)` for element presence

**Examples of Correct Usage:**
```go
// Length checks
c.Assert(users, qt.HasLen, 3)
c.Assert(jobID, qt.HasLen, 16) // for string lengths
c.Assert(errors, qt.HasLen, 0)

// Nil checks
c.Assert(user, qt.Not(qt.IsNil))
c.Assert(err, qt.IsNil)

// Boolean checks
c.Assert(user.Verified, qt.IsFalse)
c.Assert(status.Success, qt.IsTrue)

// Content checks
c.Assert(response.Message, qt.Contains, "success")
c.Assert(userList, qt.Contains, expectedUser)
```

**Anti-patterns to AVOID:**
```go
// ❌ Wrong - causes maintenance issues
c.Assert(len(users), qt.Equals, 3)
c.Assert(len(jobID), qt.Equals, 16)
c.Assert(user == nil, qt.Equals, false)
c.Assert(status.Success, qt.Equals, true)
```
