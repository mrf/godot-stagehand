// The hooks environment has Uint8Array base64 (claude-code.d.ts uses it in its
// own examples), but `claude plugin validate` type-checks modules at lib
// es2023, which lacks it, and reads no tsconfig of the plugin's.
interface Uint8Array {
  toBase64(): string
}
interface Uint8ArrayConstructor {
  fromBase64(base64: string): Uint8Array<ArrayBuffer>
}
