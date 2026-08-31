// Command migrate is retained only to give existing local users a clear migration
// path. Supabase CLI migrations in backend/supabase are authoritative.
package main

import (
	"log"
)

func main() {
	log.Fatal("the embedded Go migration runner is retired; use `pnpm supabase db reset --workdir backend` for a disposable local stack or the approved Supabase CLI deployment workflow")
}
