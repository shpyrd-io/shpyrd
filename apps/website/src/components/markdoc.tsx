import Link from "next/link";
import { BookOpen, Info, LayoutDashboard, Rocket, TriangleAlert, Workflow } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Card, CardDescription, CardHeader, CardTitle } from "@shpyrd/ui/components/card";
import { IDE } from "@shpyrd/ui/components/ide";

// What draws each tag and node of a text. A text names what it wants
// ("callout", a fenced block of code); which component of design/ui draws
// it is decided here, in one place.

function Callout({
  title,
  type = "note",
  children,
}: {
  title?: string;
  type?: "note" | "warning";
  children: React.ReactNode;
}) {
  return (
    <Alert>
      {type === "warning" ? <TriangleAlert className="text-warning" /> : <Info />}
      {title && <AlertTitle>{title}</AlertTitle>}
      <AlertDescription>{children}</AlertDescription>
    </Alert>
  );
}

function QuickLinks({ children }: { children: React.ReactNode }) {
  return <div className="grid gap-4 sm:grid-cols-2">{children}</div>;
}

// The icons the texts name, and the ones of the library that stand for them.
const icons: Record<string, React.ReactElement> = {
  installation: <Rocket />,
  presets: <Workflow />,
  theming: <LayoutDashboard />,
  plugins: <BookOpen />,
};

function QuickLink({
  title,
  description,
  href,
  icon,
}: {
  title: string;
  description: string;
  href: string;
  icon?: string;
}) {
  return (
    <Link href={href} className="group rounded-xl outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
      <Card className="h-full transition-colors group-hover:border-primary/50">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 [&_svg]:size-4 [&_svg]:text-primary">
            {icon && icons[icon]}
            {title}
          </CardTitle>
          <CardDescription>{description}</CardDescription>
        </CardHeader>
      </Card>
    </Link>
  );
}

function Fence({ content, language }: { content: string; language?: string }) {
  return <IDE code={content.replace(/\n$/, "")} language={language} showLineNumbers={false} />;
}

export const components = { Callout, QuickLinks, QuickLink, Fence };
