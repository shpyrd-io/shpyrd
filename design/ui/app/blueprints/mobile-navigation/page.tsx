import { MobileNavigation, MobileNavigationTrigger, MobileNavigationContent } from "@shpyrd/ui/components/mobile-navigation";
import { NavList, NavListItem } from "@shpyrd/ui/components/nav-list";
export default function Page() { return <div className="rounded-lg border"><MobileNavigation><div className="p-3"><MobileNavigationTrigger /></div><MobileNavigationContent><NavList aria-label="Example navigation"><NavListItem href="/" aria-current="page">Overview</NavListItem><NavListItem href="/structures/card">Cards</NavListItem></NavList></MobileNavigationContent></MobileNavigation></div>; }
