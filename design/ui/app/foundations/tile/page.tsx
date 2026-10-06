import { Cloud, FileText } from "lucide-react";
import { Tile } from "@shpyrd/ui/components/tile";
export default function Page() { return <div className="flex flex-wrap items-center gap-4"><Tile size="sm"><FileText aria-hidden /></Tile><Tile><FileText aria-hidden /></Tile><Tile size="lg"><FileText aria-hidden /></Tile><Tile variant="muted"><Cloud aria-hidden /></Tile></div>; }
