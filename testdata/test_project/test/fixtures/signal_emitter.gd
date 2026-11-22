extends Node
## Test fixture: declares signals of varying arity so waiter tests can check
## that wait_for_signal reports the declared arguments faithfully.

@warning_ignore_start("unused_signal")
signal no_args()
signal two_args(first: Variant, second: Variant)
signal trailing_null(first: Variant, second: Variant)
signal eight_args(a1: Variant, a2: Variant, a3: Variant, a4: Variant, a5: Variant, a6: Variant, a7: Variant, a8: Variant)
signal nine_args(a1: Variant, a2: Variant, a3: Variant, a4: Variant, a5: Variant, a6: Variant, a7: Variant, a8: Variant, a9: Variant)
@warning_ignore_restore("unused_signal")
