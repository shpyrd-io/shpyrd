"use client";

// The tour's slides, in order (content/site/tour.ts names them).
import { Access } from "./access";
import { Audiences } from "./audiences";
import { Closing } from "./closing";
import { Deck } from "./deck";
import { Delivery } from "./delivery";
import { Flow } from "./flow";
import { Governance } from "./governance";
import { Opening } from "./opening";
import { Platform } from "./platform";
import { Problem } from "./problem";
import { Sleep } from "./sleep";
import { Xray } from "./xray";

export function Tour() {
  return (
    <Deck
      slides={[
        (go) => <Opening next={() => go(1)} />,
        () => <Problem />,
        () => <Xray />,
        () => <Platform />,
        () => <Flow />,
        () => <Access />,
        () => <Sleep />,
        () => <Delivery />,
        () => <Governance />,
        () => <Audiences />,
        () => <Closing />,
      ]}
    />
  );
}
