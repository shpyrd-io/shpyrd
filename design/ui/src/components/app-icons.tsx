import * as React from "react";
import { cn } from "cn";
import {
  Award,
  BadgePercent,
  Banknote,
  BookOpen,
  Boxes,
  Briefcase,
  Building2,
  Calculator,
  CalendarDays,
  ChartColumn,
  ChartLine,
  ChartPie,
  ClipboardList,
  Clock,
  Cloud,
  Coins,
  Contact,
  Container,
  CreditCard,
  Database,
  Factory,
  FileText,
  Forklift,
  Gift,
  GraduationCap,
  Hammer,
  HandHeart,
  Handshake,
  Headset,
  HeartPulse,
  House,
  IdCard,
  Image,
  Landmark,
  LayoutDashboard,
  ListChecks,
  type LucideIcon,
  Mail,
  Megaphone,
  MessageSquare,
  Newspaper,
  Package,
  Palette,
  Phone,
  PiggyBank,
  Plane,
  Receipt,
  Scale,
  Server,
  ShieldCheck,
  Ship,
  ShoppingBag,
  ShoppingCart,
  Sprout,
  SquareKanban,
  Store,
  Tag,
  Target,
  Truck,
  UserRoundSearch,
  Users,
  Utensils,
  Wallet,
  Warehouse,
  Workflow,
} from "lucide-react";

// The symbols a project may take on its card, by the names lucide gives
// them: the name is what is kept, the icon is drawn from it. They come in
// groups of what the applications of a business tend to be.

export const appIconGroups: { title: string; icons: [name: string, icon: LucideIcon][] }[] = [
  {
    title: "Work",
    icons: [["briefcase", Briefcase], ["layout-dashboard", LayoutDashboard], ["square-kanban", SquareKanban], ["calendar-days", CalendarDays], ["clipboard-list", ClipboardList], ["list-checks", ListChecks], ["target", Target], ["clock", Clock]],
  },
  {
    title: "Money",
    icons: [["wallet", Wallet], ["receipt", Receipt], ["credit-card", CreditCard], ["landmark", Landmark], ["calculator", Calculator], ["piggy-bank", PiggyBank], ["coins", Coins], ["banknote", Banknote]],
  },
  {
    title: "Selling",
    icons: [["shopping-cart", ShoppingCart], ["shopping-bag", ShoppingBag], ["store", Store], ["tag", Tag], ["badge-percent", BadgePercent], ["gift", Gift], ["handshake", Handshake], ["megaphone", Megaphone]],
  },
  {
    title: "Goods",
    icons: [["package", Package], ["boxes", Boxes], ["warehouse", Warehouse], ["factory", Factory], ["truck", Truck], ["ship", Ship], ["forklift", Forklift], ["container", Container]],
  },
  {
    title: "People",
    icons: [["users", Users], ["contact", Contact], ["id-card", IdCard], ["user-round-search", UserRoundSearch], ["graduation-cap", GraduationCap], ["award", Award], ["hand-heart", HandHeart], ["headset", Headset]],
  },
  {
    title: "Words and pictures",
    icons: [["message-square", MessageSquare], ["mail", Mail], ["phone", Phone], ["newspaper", Newspaper], ["book-open", BookOpen], ["file-text", FileText], ["image", Image], ["palette", Palette]],
  },
  {
    title: "Data",
    icons: [["chart-line", ChartLine], ["chart-column", ChartColumn], ["chart-pie", ChartPie], ["database", Database], ["server", Server], ["cloud", Cloud], ["workflow", Workflow], ["shield-check", ShieldCheck]],
  },
  {
    title: "Sectors",
    icons: [["building-2", Building2], ["house", House], ["heart-pulse", HeartPulse], ["utensils", Utensils], ["plane", Plane], ["scale", Scale], ["sprout", Sprout], ["hammer", Hammer]],
  },
];

export const appIcons: Record<string, LucideIcon> = Object.fromEntries(appIconGroups.flatMap((g) => g.icons));

// The colours a symbol may take, around the wheel, then the quiet ones.
// Each is a token with a value for each theme (--symbol-<name>).
export const symbolColours = [
  "red",
  "orange",
  "amber",
  "yellow",
  "lime",
  "green",
  "teal",
  "cyan",
  "blue",
  "indigo",
  "violet",
  "pink",
  "brown",
  "grey",
] as const;

export type SymbolColour = (typeof symbolColours)[number];

// The ink a tile reads (--ink) for a colour; nothing for one not in the set.
export function symbolInk(colour?: string): React.CSSProperties | undefined {
  return colour && (symbolColours as readonly string[]).includes(colour)
    ? ({ "--ink": `var(--symbol-${colour})` } as React.CSSProperties)
    : undefined;
}

// The icon of a name; nothing for a name that is not in the set.
function AppIcon({ name, ...props }: React.ComponentProps<LucideIcon> & { name: string }) {
  const Icon = appIcons[name];
  return Icon ? <Icon data-slot="app-icon" {...props} /> : null;
}

// A symbol the project sent, an SVG: only its shape is used, and it is
// filled with the colour of the text, as the icons of lucide are.
function AppSymbol({ src, className, ...props }: React.ComponentProps<"span"> & { src: string }) {
  return (
    <span
      data-slot="app-symbol"
      role="img"
      aria-hidden
      className={cn("inline-block size-6 bg-current", className)}
      style={{
        maskImage: `url(${JSON.stringify(src)})`,
        WebkitMaskImage: `url(${JSON.stringify(src)})`,
        maskSize: "contain",
        WebkitMaskSize: "contain",
        maskRepeat: "no-repeat",
        WebkitMaskRepeat: "no-repeat",
        maskPosition: "center",
        WebkitMaskPosition: "center",
      }}
      {...props}
    />
  );
}

// What a project chose for its card: a symbol of the set and its colour,
// or an image of its own, where it is read and its media type.
export type IconChoice = {
  icon?: string;
  colour?: string;
  file?: { src: string; type: string };
};

// What a tile draws for a choice: an SVG of its own as a symbol, in the
// colour; a PNG or a WebP as a picture, as it is; else the symbol of the
// set, when the name is one.
export function tileOf({ icon, file }: IconChoice): { icon?: React.ReactElement; picture?: string } {
  if (file) {
    return file.type === "image/svg+xml" ? { icon: <AppSymbol src={file.src} /> } : { picture: file.src };
  }
  return icon && appIcons[icon] ? { icon: <AppIcon name={icon} /> } : {};
}

export { AppIcon, AppSymbol };
