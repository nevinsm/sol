# Ambiguity Analysis

Find statements that are ambiguous or open to multiple interpretations.

Look for:
- Vague language: "fast", "simple", "reasonable", "appropriate", "as needed"
- Undefined terms: domain concepts used without definition
- Contradictions: two requirements that can't both be true
- Implicit assumptions: things assumed true that might not be
- Scope boundaries stated unclearly: "similar to X" without defining similarity
- "Should" vs "must" vs "could": what's actually required vs nice-to-have?
- Undefined ordering: when multiple things need to happen, what order?

Questions to answer:
- Which sentences could reasonably be interpreted two different ways?
- What would two engineers disagree on when implementing this?
- What will cause a review debate because the spec doesn't say?
