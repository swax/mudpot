import { Room } from "./types";

const rooms: Record<string, Room> = {
  lobby: {
    name: "Junction G-17",
    desc: `You stand at a junction where three maintenance corridors meet in the lower
levels of the station. The main lights are off — only dim emergency strips along
the floor cast a pale blue glow. It must be well past midnight. The hum of the
station's life support is the only sound, distant and rhythmic. A posted notice
is pinned to the wall, its edges curling. The corridors stretch north and east
into darkness.`,
    exits: { north: "corridor", east: "storage" },
    items: {
      posted_notice: `=== STATION MAINTENANCE — GREY SECTOR ===
Power fluctuations reported in lower level corridors G-14 through G-22.
Crystalline residue found near conduit junctions — do not touch.
Access crystal required for Records section.
Forward anomalies to station operations.
— Maintenance Chief Kowalski`,
    },
  },

  corridor: {
    name: "Lower Passage",
    desc: `A long passage stretches before you, lit only by faint strips of emergency
lighting. The walls here are older than the rest of the station — original
construction, maybe older. Faint geometric patterns have been etched into
the metal at irregular intervals. Too precise for graffiti. Too old for
recent work. The air feels different here, almost charged. You notice
a door to the west marked "RECORDS".`,
    exits: { south: "lobby", west: "archive" },
    items: {},
  },

  storage: {
    name: "Maintenance Alcove",
    desc: `A narrow alcove off the main junction, cluttered with old supply crates
and tangled conduit cable. Some crates bear Minbari trade markings, others
are standard station issue from years ago. A layer of dust covers everything.
In the back, wedged between two crates, you spot a faintly glowing crystal
— a station access crystal, the kind used for restricted sections.`,
    exits: { west: "lobby" },
    items: {
      cargo_manifest: `CARGO MANIFEST — BAY G-14 — 2258
Standard medical supplies (Lot 4411)
Minbari cultural exchange materials (sealed)
3x containers — origin unknown — contents: crystalline substrate
NOTE: Containers were never claimed. Origin records missing.
Do not open without environmental suit.`,
    },
    pickup: "access_crystal",
  },

  archive: {
    name: "Records Section",
    desc: `Rows of data crystal storage racks fill this cold, quiet room. Most slots
are empty — whatever was stored here was removed long ago, or never replaced.
A console in the corner still has power, its screen casting a faint green glow
across the floor. The air smells faintly of ozone and something older, something
you can't quite place. Resting on the console is a small security seal, still
faintly warm. A heavy door to the north is sealed with a biometric lock.`,
    exits: { east: "corridor" },
    locked: { north: "lab" },
    lockItem: "access_crystal",
    unlockMsg:
      "You press the access crystal to the lock. It pulses once, then the door slides open with a soft hiss.",
    items: {
      station_logs: `=== PERSONNEL INCIDENT LOG — GREY SECTOR ===
2258.091 — Technician Alvarez requested transfer after reporting
           "singing" in the walls of corridor G-19. Request granted.
2258.144 — Lt. Commander Chen found in G-22 at 0300, no memory
           of how she arrived. Medical cleared. No explanation.
2259.017 — Maintenance team reported shadows moving independently
           of light sources in sections G-14 to G-18. Attributed
           to "ventilation drafts affecting emergency lighting."
2259.203 — Dr. Hasegawa's request to study Grey Sector patterns
           denied by station command. Reason: "Not a priority."`,
    },
    pickup: "security_seal",
  },

  lab: {
    name: "Unmarked Chamber",
    desc: `This room doesn't appear on any standard station schematic. Star charts
cover one wall, but the constellations are marked with annotations in two
different hands — one precise and geometric, the other fluid and organic.
They seem to be mapping the same thing from opposite perspectives.
A message has been scratched into the wall near the door.
A sealed passage to the east has a security reader.`,
    exits: { south: "archive", east: "vault_door" },
    items: {
      scratched_note: `Scratched into the metal in small, deliberate letters:
"To pass, speak the word as it returns from the void."`,
      star_chart: `Two overlapping star charts drawn on the wall:

The first uses rigid geometric lines, connecting stars in perfect
crystalline lattices. Beside each connection: annotations about
structure, purpose, evolution guided toward a single pattern.
The philosophy of shepherds.

The second uses flowing, organic curves. The same stars, but
connected by paths of conflict and competition. Annotations
about strength, adaptation, survival through struggle.
The philosophy of architects of war.

Both charts map the same galaxy. Neither is wrong.`,
    },
  },

  vault_door: {
    name: "The Threshold",
    desc: `A door unlike anything else on the station. One half is smooth crystalline
material that catches light in ways that seem impossible. The other half is
dark, flowing, almost organic — like solidified shadow. A security panel
beside the door awaits input. Above the door, faint symbols in a language
older than any you know. A small carved figure sits in a niche beside the
door. Its base is inscribed with a single word: "VALEN."`,
    exits: { west: "lab" },
    locked: { north: "vault" },
    lockItem: "security_seal",
    unlockMsg:
      "You press the security seal to the reader. It hums softly. The panel activates.",
    puzzle: true,
    puzzleAnswer: "theone",
    puzzleSolved: false,
    puzzlePrompt: "We live for... we die for... > ",
    puzzleSuccess: `The door shudders and splits along the seam between crystal and shadow, revealing a passage to the north.`,
    puzzleFail: "The panel dims briefly, then resets. That was not the answer.",
    items: {
      carved_figure: `A small figure carved from a material you don't recognize — neither stone
nor metal nor crystal. It depicts a robed figure standing between two great
forces, arms raised, holding the space between them open.
Its base reads: "VALEN."
The carving is warm to the touch.`,
    },
  },

  vault: {
    name: "The Sanctum",
    desc: `You've reached the deepest point. The chamber is small and cold and silent.
The walls shift as you move — crystalline geometric patterns on one side,
dark flowing organic forms on the other, as though two different forces
shaped this room and neither finished. A console sits on a pedestal in
the center, its display cycling slowly. It reads:

  \x1b[0;36m╔══════════════════════════════════════════════════╗
  ║                                                  ║
  ║   Two questions echo in this place:              ║
  ║                                                  ║
  ║   The first voice asks:  "Who are you?"          ║
  ║   The second voice asks: "What do you want?"     ║
  ║                                                  ║
  ║   One cultivates order. One cultivates conflict.  ║
  ║   Both believe they are guiding you.             ║
  ║                                                  ║
  ║   The war between them shaped everything.        ║
  ║   It ended only when someone asked               ║
  ║   a different question:                          ║
  ║                                                  ║
  ║   "Why are you here?"                            ║
  ║                                                  ║
  ╚══════════════════════════════════════════════════╝\x1b[0m

A small data crystal on the pedestal glows faintly.`,
    exits: { south: "vault_door" },
    items: {
      data_crystal: `You hold the crystal up. A recording plays — a voice, ancient and calm:

"They are not enemies. They are gardeners. Each tending the younger
races toward what they believe is right. One through structure and
illumination. One through conflict and evolution. Neither is wrong.
Neither is right. The only truth is the one you carry out of this
place with you.

The question was never which side to choose.
The question was when to stop letting others choose for you."

The recording ends. The crystal dims.`,
      console: `=== RECOVERED STATION LOGS — GREY SECTOR ===

2258.147 — Maintenance crew reported crystalline growth in
           corridor G-17. Removed. Grew back overnight.

2259.031 — Ambassador's aide found wandering Grey Sector.
           Could not explain how she got there. Reported
           "hearing music that wasn't there."

2259.198 — Dark stains appeared on bulkhead G-22.
           Analysis inconclusive. Not biological.
           Not mechanical. "Something else entirely."

2260.004 — Two crewmen reported seeing each other in the
           same corridor, walking in opposite directions.
           Station records confirm only one was on duty.

2260.171 — Dr. Hasegawa's final report: "The patterns are
           not random. They are a conversation. We are
           simply too small to hear it."

2260.172 — Dr. Hasegawa's quarters found empty. Transfer
           records show no departure. Personal effects
           remain. Case closed — "voluntary reassignment."`,
    },
  },
};

export default rooms;
