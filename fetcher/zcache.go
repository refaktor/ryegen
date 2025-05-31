package fetcher

type cache struct {
	// compatibleVersion is used for simple cache
	// invalidation, in case a breaking change is made.
	compatibleVersion int
}
