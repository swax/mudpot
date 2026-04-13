const { createSession, handleInput, look } = require("./game");

// Suppress logging during tests
const log = require("./log");
const origLog = log;
require.cache[require.resolve("./log")].exports = function () {};

let failed = 0;
let passed = 0;

function assert(condition, msg) {
  if (!condition) {
    console.error(`  FAIL: ${msg}`);
    failed++;
  } else {
    console.log(`  pass: ${msg}`);
    passed++;
  }
}

function assertIncludes(str, substr, msg) {
  assert(str.includes(substr), msg || `expected output to include "${substr}"`);
}

function assertNotIncludes(str, substr, msg) {
  assert(
    !str.includes(substr),
    msg || `expected output not to include "${substr}"`,
  );
}

// Strip ANSI escape codes for easier matching
function strip(str) {
  return str.replace(/\x1b\[[0-9;]*m/g, "");
}

console.log("\n=== Mudpot Victory Walkthrough Test ===\n");

const session = createSession("test");

// 1. Start in the lobby
console.log("-- Starting in lobby --");
let out = strip(look(session));
assertIncludes(out, "Junction G-17", "starts in Junction G-17");
assertIncludes(out, "north", "lobby has north exit");
assertIncludes(out, "east", "lobby has east exit");

// 2. Go east to storage, pick up access_crystal
console.log("\n-- Go east to storage --");
out = strip(handleInput(session, "east"));
assertIncludes(out, "Maintenance Alcove", "entered storage");
assert(session.room === "storage", "session.room is storage");

console.log("\n-- Pick up access_crystal --");
out = strip(handleInput(session, "take access_crystal"));
assertIncludes(out, "access_crystal", "picked up access_crystal");
assert(
  session.inventory.includes("access_crystal"),
  "access_crystal in inventory",
);

// 3. Go back west to lobby, then north to corridor
console.log("\n-- Go west to lobby --");
out = strip(handleInput(session, "west"));
assertIncludes(out, "Junction G-17", "back in lobby");

console.log("\n-- Go north to corridor --");
out = strip(handleInput(session, "north"));
assertIncludes(out, "Lower Passage", "entered corridor");

// 4. Go west to archive
console.log("\n-- Go west to archive --");
out = strip(handleInput(session, "west"));
assertIncludes(out, "Records Section", "entered archive");

// 5. Pick up security_seal
console.log("\n-- Pick up security_seal --");
out = strip(handleInput(session, "take security_seal"));
assertIncludes(out, "security_seal", "picked up security_seal");
assert(
  session.inventory.includes("security_seal"),
  "security_seal in inventory",
);

// 6. Use access_crystal to unlock north door, then go north to lab
console.log("\n-- Use access_crystal to unlock north --");
out = strip(handleInput(session, "use access_crystal"));
assertIncludes(out, "slides open", "unlock message shown");

console.log("\n-- Go north to lab --");
out = strip(handleInput(session, "north"));
assertIncludes(out, "Unmarked Chamber", "entered lab");

// 7. Verify using access_crystal again doesn't add undefined exit
console.log("\n-- Go south back to archive --");
out = strip(handleInput(session, "south"));
assertIncludes(out, "Records Section", "back in archive");

console.log("\n-- Use access_crystal again (already unlocked) --");
out = strip(handleInput(session, "use access_crystal"));
assertNotIncludes(out, "slides open", "no unlock message for already unlocked");

out = strip(look(session));
assertNotIncludes(out, "undefined", "no undefined in exits after double use");

// 8. Go north to lab, then east to vault_door
console.log("\n-- Go north to lab again --");
out = strip(handleInput(session, "north"));
assertIncludes(out, "Unmarked Chamber", "entered lab");

console.log("\n-- Go east to vault_door --");
out = strip(handleInput(session, "east"));
assertIncludes(out, "The Threshold", "entered vault_door");

// 9. Use security_seal — should trigger puzzle
console.log("\n-- Use security_seal to activate panel --");
out = strip(handleInput(session, "use security_seal"));
assertIncludes(out, "We live for", "puzzle prompt shown");
assert(session.inputMode === "puzzle", "session in puzzle mode");

// 10. Enter wrong answer first
console.log("\n-- Enter wrong puzzle answer --");
out = strip(handleInput(session, "valen"));
assertIncludes(out, "not the answer", "wrong answer rejected");
assert(session.inputMode === "puzzle", "still in puzzle mode");

// 11. Enter correct answer "the one"
console.log("\n-- Enter correct puzzle answer --");
out = strip(handleInput(session, "the one"));
assertIncludes(out, "door shudders", "puzzle success message");
assert(session.inputMode === "normal", "back to normal mode");

// 12. Go north to vault
console.log("\n-- Go north to vault --");
out = strip(handleInput(session, "north"));
assertIncludes(out, "The Sanctum", "entered the vault");
assert(session.room === "vault", "session.room is vault");

// 13. Read data_crystal — should trigger victory and disconnect
console.log("\n-- Read data_crystal for victory --");
out = strip(handleInput(session, "read data_crystal"));
assert(out.startsWith("VICTORY"), "returns VICTORY signal for disconnect");
assertIncludes(out, "They are not enemies", "crystal text shown");
assertIncludes(out, "heart of Grey Sector", "victory message shown");

console.log(`\n=== Results: ${passed} passed, ${failed} failed ===\n`);
process.exit(failed > 0 ? 1 : 0);
