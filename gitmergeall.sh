#!/bin/bash

branches=(
  "hmm-regime-study-3568657603859447931"
  "origin/feat/git-unmerged-tracker-435091735500454003"
  "origin/hmm-regime-study-3568657603859447931"
  "origin/jules-10356927943885384019-5fa62703"
  "origin/remove-python-architecture-proposals-12770349860443076407"
  "origin/update-readme-capabilities-15043874155249407223"
)

successful=()
failed=()

for branch in "${branches[@]}"; do
  echo "--------------------------------------------------"
  echo "Attempting merge: $branch"

  if git merge --no-ff "$branch" -m "Merge branch $branch"; then
    echo "✓ Success: $branch"
    successful+=("$branch")
  else
    echo "✗ Conflict detected! Aborting merge for: $branch"
    git merge --abort
    failed+=("$branch")
  fi
done

echo ""
echo "=================================================="
echo "                   MERGE SUMMARY                  "
echo "=================================================="
echo -e "\n✅ Succeeded (${#successful[@]}):"
for b in "${successful[@]}"; do
  echo "   - $b"
done

echo -e "\n❌ Failed / Conflicts (${#failed[@]}):"
for b in "${failed[@]}"; do
  echo "   - $b"
done
echo "=================================================="
