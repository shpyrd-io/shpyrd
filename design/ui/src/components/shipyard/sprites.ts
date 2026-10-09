// The pieces of the shipyard, a file for each, and where each one stands.
//
// A piece is drawn as it is seen from above and from the side at once
// (isometric): a step across the quay (x) goes right and down, a step
// along it (y) goes left and down, a step up (z) goes up. What goes
// somewhere goes down and to the left: a truck and a ship show their
// nose there.
//
// Each piece in use has a dark twin (name.dark.svg): the same drawing with
// black for its white, the theme's dark greys for its light ones and a
// darker grey for its lines, its orange kept. To replace a piece, put the
// new file in its place, make its twin, and say three things of it here:
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
import containerDark from "./sprites/container.dark.svg";
import craneAframe from "./sprites/crane-aframe.svg";
import craneAframeDark from "./sprites/crane-aframe.dark.svg";
import craneGirderBack from "./sprites/crane-girder-back.svg";
import craneGirderBackDark from "./sprites/crane-girder-back.dark.svg";
import craneGirderFront from "./sprites/crane-girder-front.svg";
import craneGirderFrontDark from "./sprites/crane-girder-front.dark.svg";
import craneHouse from "./sprites/crane-house.svg";
import craneHouseDark from "./sprites/crane-house.dark.svg";
import craneLandBack from "./sprites/crane-land-back.svg";
import craneLandBackDark from "./sprites/crane-land-back.dark.svg";
import craneLandFront from "./sprites/crane-land-front.svg";
import craneLandFrontDark from "./sprites/crane-land-front.dark.svg";
import craneRails from "./sprites/crane-rails.svg";
import craneRailsDark from "./sprites/crane-rails.dark.svg";
import craneSeaB from "./sprites/crane-sea-b.svg";
import craneSeaBDark from "./sprites/crane-sea-b.dark.svg";
import craneSeaD from "./sprites/crane-sea-d.svg";
import craneSeaDDark from "./sprites/crane-sea-d.dark.svg";
import craneSeaLow from "./sprites/crane-sea-low.svg";
import craneSeaLowDark from "./sprites/crane-sea-low.dark.svg";
import craneSpreader from "./sprites/crane-spreader.svg";
import craneSpreaderDark from "./sprites/crane-spreader.dark.svg";
import craneTrolley from "./sprites/crane-trolley.svg";
import craneTrolleyDark from "./sprites/crane-trolley.dark.svg";
import gate from "./sprites/gate.svg";
import gateDark from "./sprites/gate.dark.svg";
import ground from "./sprites/ground.svg";
import groundDark from "./sprites/ground.dark.svg";
import shipBridge from "./sprites/ship-bridge.svg";
import shipBridgeDark from "./sprites/ship-bridge.dark.svg";
import shipHull from "./sprites/ship-hull.svg";
import shipHullDark from "./sprites/ship-hull.dark.svg";
import sign from "./sprites/sign.svg";
import truckBed from "./sprites/truck-bed.svg";
import truckBedDark from "./sprites/truck-bed.dark.svg";
import truckCab from "./sprites/truck-cab.svg";
import truckCabDark from "./sprites/truck-cab.dark.svg";
import warehouse from "./sprites/warehouse.svg";

// A file as whoever bundles the code gives it: its address, or something
// that has it.
type File = string | { src: string };
const src = (file: File) => (typeof file === "string" ? file : file.src);

type Piece = { src: string; dark?: string; size: readonly [number, number]; stands: readonly [number, number]; unit: number };

// Where every part of the crane stands, in its drawing.
const crane = { size: [504.99, 411.59], stands: [170.3, 359.75], unit: 51.9 } as const;

export const sprites = {
  // Basic pieces, still to be drawn. What is made of parts has a file for
  // each part, so that each is drawn before or after what stands beside
  // it: a container on a truck is over its bed and behind its cab. The
  // parts of a truck stand on the same point, and so do those of a ship.
  "crane-spreader": { src: src(craneSpreader), dark: src(craneSpreaderDark), size: [134, 87], stands: [67, 47], unit: 50 },
  warehouse: { src: src(warehouse), size: [446, 376], stands: [223, 251], unit: 50 },
  // The gate stands on the strip between the warehouse and the kerb of the
  // drawn yard, which is narrower than it was: it is drawn at 0.65.
  gate: { src: src(gate), dark: src(gateDark), size: [100, 125], stands: [50, 100], unit: 76.9 },
  // Drawn pieces. The container is drawn a little larger than the first
  // one was: its unit is its short side, across, so that it stays as big.
  container: { src: src(container), dark: src(containerDark), size: [134.28, 131.11], stands: [67.14, 92.04], unit: 51.9 },
  // The truck is one drawing cut in two, standing on the same point: the
  // bed, under the container it carries, and the cab before it. Its unit is
  // the bed, one container wide; its point, the middle of the bed on the ground.
  "truck-bed": { src: src(truckBed), dark: src(truckBedDark), size: [234.65, 162.83], stands: [147.22, 78.15], unit: 60.9 },
  "truck-cab": { src: src(truckCab), dark: src(truckCabDark), size: [234.65, 162.83], stands: [147.22, 78.15], unit: 60.9 },
  // The yard itself, drawn whole: its pads, the stacks, the warehouse, the
  // sign, the lane and the quay. Placed by the crane's rails: their middle is
  // the lane the trucks take, at the scale of the containers.
  ground: { src: src(ground), dark: src(groundDark), size: [1200, 1200], stands: [341.9, 554.8], unit: 51.9 },
  // The crane is one drawing cut in nine files that stand on the same point,
  // the ground under the middle of its four legs, so that what moves can go
  // between its parts: a truck between its legs, a container between the
  // legs on the water side, the trolley between its two girders. Its legs
  // stand on the rails of the ground, at the same scale.
  "crane-land-back": { src: src(craneLandBack), dark: src(craneLandBackDark), ...crane },
  "crane-girder-back": { src: src(craneGirderBack), dark: src(craneGirderBackDark), ...crane },
  "crane-sea-low": { src: src(craneSeaLow), dark: src(craneSeaLowDark), ...crane },
  "crane-sea-b": { src: src(craneSeaB), dark: src(craneSeaBDark), ...crane },
  "crane-girder-front": { src: src(craneGirderFront), dark: src(craneGirderFrontDark), ...crane },
  "crane-rails": { src: src(craneRails), dark: src(craneRailsDark), ...crane },
  "crane-land-front": { src: src(craneLandFront), dark: src(craneLandFrontDark), ...crane },
  "crane-sea-d": { src: src(craneSeaD), dark: src(craneSeaDDark), ...crane },
  "crane-aframe": { src: src(craneAframe), dark: src(craneAframeDark), ...crane },
  "crane-house": { src: src(craneHouse), dark: src(craneHouseDark), ...crane },
  // The trolley hangs under the rails, between the girders, drawn here in
  // the crane's colours.
  "crane-trolley": { src: src(craneTrolley), dark: src(craneTrolleyDark), size: [128.5, 84.19], stands: [61.33, 271.1], unit: 51.9 },
  // The ship is one drawing, cut in two files that stand on the same point:
  // the hull, with the water moving against it, and the bridge over it. Its
  // unit is its deck, three containers wide, as the first ship's was.
  "ship-hull": { src: src(shipHull), dark: src(shipHullDark), size: [705.08, 544.4], stands: [344.95, 342.22], unit: 55.35 },
  "ship-bridge": { src: src(shipBridge), dark: src(shipBridgeDark), size: [705.08, 544.4], stands: [344.95, 342.22], unit: 55.35 },
  sign: { src: src(sign), size: [1024, 1024], stands: [575, 850], unit: 135 },
} satisfies Record<string, Piece>;

export type Sprite = keyof typeof sprites;
