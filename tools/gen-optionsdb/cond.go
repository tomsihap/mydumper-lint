package main

import (
	"slices"
	"sort"
	"strings"
)

// minimalCondition returns the shortest sum-of-products formula over the n
// known macros that is true exactly for the configurations listed (indexes
// into allConfigs: bit i is knownMacros[i]). It returns "" when the formula
// is always true. Literals follow the order of knownMacros; terms are joined
// with " || " in lexical order.
func minimalCondition(minterms []int, n int) string {
	full := 1 << n
	on := make([]bool, full)
	for _, m := range minterms {
		on[m] = true
	}
	if !slices.Contains(on, false) {
		return ""
	}
	// Every implicant is (value, care): the configurations c with
	// c&care == value&care. Keep those that only cover true configurations.
	type imp struct{ value, care int }
	covers := func(p imp, c int) bool { return c&p.care == p.value&p.care }
	var imps []imp
	for care := 0; care < full; care++ {
		for value := 0; value < full; value++ {
			if value&^care != 0 {
				continue
			}
			p := imp{value, care}
			ok, any := true, false
			for c := 0; c < full; c++ {
				if covers(p, c) {
					any = true
					if !on[c] {
						ok = false
						break
					}
				}
			}
			if ok && any {
				imps = append(imps, p)
			}
		}
	}
	// prime implicants: not strictly contained in another implicant
	var primes []imp
	for _, p := range imps {
		prime := true
		for _, q := range imps {
			if q.care != p.care && q.care&p.care == q.care && covers(q, p.value) {
				prime = false
				break
			}
		}
		if prime {
			primes = append(primes, p)
		}
	}
	render := func(p imp) string {
		var lits []string
		for i, m := range knownMacros[:n] {
			if p.care&(1<<i) == 0 {
				continue
			}
			if p.value&(1<<i) != 0 {
				lits = append(lits, m)
			} else {
				lits = append(lits, "!"+m)
			}
		}
		return strings.Join(lits, " && ")
	}
	// smallest cover: fewest terms, then fewest literals, then lexical order
	best, bestLits := "", -1
	bestTerms := len(primes) + 1
	for mask := 1; mask < 1<<len(primes); mask++ {
		var chosen []imp
		for i, p := range primes {
			if mask&(1<<i) != 0 {
				chosen = append(chosen, p)
			}
		}
		if len(chosen) > bestTerms {
			continue
		}
		complete := true
		for c := 0; c < full && complete; c++ {
			if !on[c] {
				continue
			}
			hit := false
			for _, p := range chosen {
				if covers(p, c) {
					hit = true
				}
			}
			complete = hit
		}
		if !complete {
			continue
		}
		lits := 0
		terms := make([]string, 0, len(chosen))
		for _, p := range chosen {
			lits += popcount(p.care)
			terms = append(terms, render(p))
		}
		sort.Strings(terms)
		s := strings.Join(terms, " || ")
		if len(chosen) < bestTerms || lits < bestLits || (lits == bestLits && s < best) {
			best, bestLits, bestTerms = s, lits, len(chosen)
		}
	}
	return best
}

func popcount(x int) int {
	n := 0
	for ; x != 0; x &= x - 1 {
		n++
	}
	return n
}
