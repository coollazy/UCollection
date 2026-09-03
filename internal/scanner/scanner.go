// Package scanner runs the on-chain event scanning background tasks. See
// docs/開發流程框架-03-技術架構設計.md 第4節.
package scanner

import "context"

// Run blocks until ctx is cancelled. It is a placeholder for the three
// always-on background tasks defined in 技術架構設計第4節 and will be
// replaced with real scanning logic in 階段05.
func Run(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
