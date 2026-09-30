// The pieces of the shipyard, a file for each, and where each one stands.
//
// A piece is drawn as it is seen from above and from the side at once
// (isometric): a step across the quay (x) goes right and down, a step
// along it (y) goes left and down, a step up (z) goes up. What goes
// somewhere goes down and to the left: a truck and a ship show their
// nose there.
//
// To replace a piece, put the new file in its place and say three things
// of it here:
//   size    the width and the height of the file, in its own units
//   stands  the point of the file where the piece stands on the ground:
//           under the middle of its base (of its bed, for the truck; of
//           its middle hatch, for the ship), in the same units
//   unit    how many of those units the width of a container takes in
//           this file: what gives every piece the same scale
//
// The types of an imported image are Next's. Next writes them into
// next-env.d.ts when it runs, but that file is not committed and another
// program (the site's typecheck) compiles this file too, so they are
// named here, where the images are imported.
/// <reference types="next/image-types/global" />
import container from "./sprites/container.svg";
import craneBeam from "./sprites/crane-beam.svg";
import craneCabin from "./sprites/crane-cabin.svg";
import craneLeg from "./sprites/crane-leg.svg";
import craneSpreader from "./sprites/crane-spreader.svg";
import craneTie from "./sprites/crane-tie.svg";
import craneTrolley from "./sprites/crane-trolley.svg";
import gate from "./sprites/gate.svg";
import ground from "./sprites/ground.svg";
import shipBridge from "./sprites/ship-bridge.svg";
import shipHull from "./sprites/ship-hull.svg";
import sign from "./sprites/sign.svg";
import truckBed from "./sprites/truck-bed.svg";
import truckCab from "./sprites/truck-cab.svg";
import warehouse from "./sprites/warehouse.svg";

// A file as whoever bundles the code gives it: its address, or something
// that has it.
type File = string | { src: string };
const src = (file: File) => (typeof file === "string" ? file : file.src);

type Piece = { src: string; size: readonly [number, number]; stands: readonly [number, number]; unit: number };

export const sprites = {
  // Basic pieces, still to be drawn. What is made of parts has a file for
  // each part, so that each is drawn before or after what stands beside
  // it: a container on a truck is over its bed and behind its cab. The
  // parts of a truck stand on the same point, and so do those of a ship.
  container: { src: src(container), size: [134, 130], stands: [67, 90], unit: 50 },
  "truck-bed": { src: src(truckBed), size: [138, 89], stands: [68, 64], unit: 50 },
  "truck-cab": { src: src(truckCab), size: [90, 116], stands: [113, 60], unit: 50 },
  "ship-hull": { src: src(shipHull), size: [650, 417], stands: [262, 265], unit: 50 },
  "ship-bridge": { src: src(shipBridge), size: [213, 224], stands: [-158, 362], unit: 50 },
  "crane-leg": { src: src(craneLeg), size: [58, 287], stands: [29, 269], unit: 50 },
  "crane-beam": { src: src(craneBeam), size: [397, 250], stands: [138, 360], unit: 50 },
  "crane-tie": { src: src(craneTie), size: [124, 86], stands: [62, 312], unit: 50 },
"crane-trolley": { src: src(craneTrolley), size: [182, 120], stands: [91, 346], unit: 50 },
  "crane-cabin": { src: src(craneCabin), size: [66, 100], stands: [128, 240], unit: 50 },
  "crane-spreader": { src: src(craneSpreader), size: [134, 87], stands: [67, 47], unit: 50 },
  warehouse: { src: src(warehouse), size: [446, 376], stands: [223, 251], unit: 50 },
  gate: { src: src(gate), size: [100, 125], stands: [50, 100], unit: 50 },
  ground: { src: src(ground), size: [2718, 1591], stands: [975, 564], unit: 50 },
  // Drawn pieces.
  sign: { src: src(sign), size: [1024, 1024], stands: [575, 850], unit: 135 },
} satisfies Record<string, Piece>;

export type Sprite = keyof typeof sprites;
