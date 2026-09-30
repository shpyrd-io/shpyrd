// The pieces of the shipyard, a file for each, and where each one stands.
//
// A piece is drawn as it is seen from above and from the side at once
// (isometric): a unit of the yard is 50 units of the drawing, a step in x goes
// right and down, a step in y goes left and down, a step in z goes up.
//
// `box` is the viewBox of the file. The point 0,0 of the drawing is where
// the piece stands on the ground: the middle of its base. To replace a
// piece, draw it around that point and copy the viewBox of the new file here.
import container from "./sprites/container.svg";
import truck from "./sprites/truck.svg";
import ship from "./sprites/ship.svg";
import craneLeg from "./sprites/crane-leg.svg";
import craneTop from "./sprites/crane-top.svg";
import craneTrolley from "./sprites/crane-trolley.svg";
import craneSpreader from "./sprites/crane-spreader.svg";
import warehouse from "./sprites/warehouse.svg";
import sign from "./sprites/sign.svg";
import gate from "./sprites/gate.svg";
import ground from "./sprites/ground.svg";

// A file as whoever bundles the code gives it: its address, or something
// that has it.
type File = string | { src: string };
const src = (file: File) => (typeof file === "string" ? file : file.src);

export type Box = readonly [x: number, y: number, width: number, height: number];

export const sprites = {
  "container": { src: src(container), box: [-67, -90, 134, 130] },
  "truck": { src: src(truck), box: [-113, -64, 183, 120] },
  "ship": { src: src(ship), box: [-262, -362, 650, 514] },
  "crane-leg": { src: src(craneLeg), box: [-29, -269, 58, 287] },
  "crane-top": { src: src(craneTop), box: [-198, -395, 517, 320] },
  "crane-trolley": { src: src(craneTrolley), box: [-115, -346, 206, 171] },
  "crane-spreader": { src: src(craneSpreader), box: [-67, -47, 134, 87] },
  "warehouse": { src: src(warehouse), box: [-223, -251, 446, 376] },
  "sign": { src: src(sign), box: [-70, -356, 131, 391] },
  "gate": { src: src(gate), box: [-50, -100, 100, 125] },
  "ground": { src: src(ground), box: [-975, -564, 2718, 1591] },
} satisfies Record<string, { src: string; box: Box }>;

export type Sprite = keyof typeof sprites;
