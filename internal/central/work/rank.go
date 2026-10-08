package work

import (
	"math/big"
	"slices"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type rankItem struct {
	ID   string
	Rank string
}
type rankResult struct {
	Items      []rankItem
	Rank       string
	Changed    bool
	Rebalanced bool
	Previous   string
	Next       string
}

func rankMaximum() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
}
func rankNumber(s string) *big.Int { n, _ := new(big.Int).SetString(s, 16); return n }
func rankText(n *big.Int) string   { s := n.Text(16); return strings.Repeat("0", 32-len(s)) + s }
func middleRank(left, right string) (string, bool) {
	l := new(big.Int)
	if left != "" {
		l = rankNumber(left)
	}
	r := rankMaximum()
	if right != "" {
		r = rankNumber(right)
	}
	m := new(big.Int).Rsh(new(big.Int).Add(l, r), 1)
	if m.Cmp(l) <= 0 || m.Cmp(r) >= 0 {
		return "", false
	}
	return rankText(m), true
}

// rankFor never mutates the input. A no-op returns the original rank bytes and
// never invokes physical maintenance. Completed spectators may be renumbered,
// but their relative order and all business fields belong to the caller.
func rankFor(input []rankItem, target, before string, create bool) (rankResult, error) {
	if _, err := f.ParseID[struct{}](target); err != nil {
		return rankResult{}, fault(f.InvalidArgument)
	}
	if before != "" {
		if _, err := f.ParseID[struct{}](before); err != nil {
			return rankResult{}, fault(f.InvalidArgument)
		}
	}
	if len(input) > c.MaxGroupSize {
		return rankResult{}, fault(f.InternalError)
	}
	if create && len(input) == c.MaxGroupSize {
		return rankResult{}, fault(f.ResourceBusy)
	}
	seen := map[string]bool{}
	targetIndex := -1
	anchorIndex := -1
	for n, row := range input {
		if _, err := f.ParseID[struct{}](row.ID); err != nil || c.ValidateRank(row.Rank) != nil || seen[row.ID] || n > 0 && input[n-1].Rank >= row.Rank {
			return rankResult{}, fault(f.InternalError)
		}
		seen[row.ID] = true
		if row.ID == target {
			targetIndex = n
		}
		if row.ID == before {
			anchorIndex = n
		}
	}
	if before == target {
		return rankResult{}, field(f.InvalidArgument, "/before_id", "SELF_ANCHOR")
	}
	if create && targetIndex >= 0 {
		return rankResult{}, fault(f.ResourceBusy)
	}
	if !create && targetIndex < 0 || before != "" && anchorIndex < 0 {
		return rankResult{}, fault(f.NotFound)
	}
	items := slices.Clone(input)
	if !create {
		items = append(items[:targetIndex], items[targetIndex+1:]...)
	}
	index := len(items)
	for n, row := range items {
		if row.ID == before {
			index = n
			break
		}
	}
	if !create && index == targetIndex {
		prev, next := "", ""
		if index > 0 {
			prev = input[index-1].ID
		}
		if index+1 < len(input) {
			next = input[index+1].ID
		}
		return rankResult{Items: slices.Clone(input), Rank: input[targetIndex].Rank, Previous: prev, Next: next}, nil
	}
	neighbors := func() (string, string) {
		left, right := "", ""
		if index > 0 {
			left = items[index-1].Rank
		}
		if index < len(items) {
			right = items[index].Rank
		}
		return left, right
	}
	left, right := neighbors()
	rank, ok := middleRank(left, right)
	rebalanced := false
	if !ok {
		// Rebalance the ORIGINAL group (including target), then recompute the
		// intended slot by identity. Only this exact physical group is touched.
		original := slices.Clone(input)
		den := big.NewInt(int64(len(original) + 1))
		max := rankMaximum()
		for n := range original {
			v := new(big.Int).Div(new(big.Int).Mul(big.NewInt(int64(n+1)), max), den)
			original[n].Rank = rankText(v)
		}
		items = original
		if !create {
			items = append(items[:targetIndex], items[targetIndex+1:]...)
		}
		left, right = neighbors()
		rank, ok = middleRank(left, right)
		if !ok {
			return rankResult{}, fault(f.InvalidState)
		}
		rebalanced = true
	}
	previous, next := "", ""
	if index > 0 {
		previous = items[index-1].ID
	}
	if index < len(items) {
		next = items[index].ID
	}
	items = append(items, rankItem{})
	copy(items[index+1:], items[index:])
	items[index] = rankItem{ID: target, Rank: rank}
	return rankResult{Items: items, Rank: rank, Changed: true, Rebalanced: rebalanced, Previous: previous, Next: next}, nil
}
