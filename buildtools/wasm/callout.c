// The one host import of pcre2.wasm, and the export that installs it.
//
// PCRE2 calls a callout function through a pointer in the match context.
// In WebAssembly that has to be a function in the module, so callout()
// forwards the block to gopcre2.callout, which the Go wrapper implements
// (see callout_wasm2go.go). It is only installed on match contexts of
// patterns compiled with OnCallout, so without it the import is never
// called.

#define PCRE2_CODE_UNIT_WIDTH 8
#include <stddef.h>
#include <pcre2.h>

__attribute__((import_module("gopcre2"), import_name("callout")))
int gopcre2_callout(pcre2_callout_block *block);

static int callout(pcre2_callout_block *block, void *data) {
	(void)data;
	return gopcre2_callout(block);
}

__attribute__((export_name("gopcre2_set_callout")))
int gopcre2_set_callout(pcre2_match_context *mctx) {
	return pcre2_set_callout(mctx, callout, NULL);
}

// The Go side reads the block at these offsets.
_Static_assert(sizeof(PCRE2_SIZE) == 4 && sizeof(void *) == 4, "wasm32");
_Static_assert(offsetof(pcre2_callout_block, version) == 0, "version");
_Static_assert(offsetof(pcre2_callout_block, callout_number) == 4, "callout_number");
_Static_assert(offsetof(pcre2_callout_block, capture_top) == 8, "capture_top");
_Static_assert(offsetof(pcre2_callout_block, capture_last) == 12, "capture_last");
_Static_assert(offsetof(pcre2_callout_block, offset_vector) == 16, "offset_vector");
_Static_assert(offsetof(pcre2_callout_block, mark) == 20, "mark");
_Static_assert(offsetof(pcre2_callout_block, start_match) == 32, "start_match");
_Static_assert(offsetof(pcre2_callout_block, current_position) == 36, "current_position");
_Static_assert(offsetof(pcre2_callout_block, pattern_position) == 40, "pattern_position");
_Static_assert(offsetof(pcre2_callout_block, next_item_length) == 44, "next_item_length");
_Static_assert(offsetof(pcre2_callout_block, callout_string_length) == 52, "callout_string_length");
_Static_assert(offsetof(pcre2_callout_block, callout_string) == 56, "callout_string");
_Static_assert(offsetof(pcre2_callout_block, callout_flags) == 60, "callout_flags");
