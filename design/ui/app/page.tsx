"use client";

import { Info, Moon, Sun } from "lucide-react";
import { Wordmark } from "@shpyrd/ui/components/brand";
import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { Badge } from "@shpyrd/ui/components/badge";
import { Button } from "@shpyrd/ui/components/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@shpyrd/ui/components/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@shpyrd/ui/components/dialog";
import { Input } from "@shpyrd/ui/components/input";
import { Label } from "@shpyrd/ui/components/label";
import { Skeleton } from "@shpyrd/ui/components/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@shpyrd/ui/components/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@shpyrd/ui/components/tabs";
import { useTheme } from "@shpyrd/ui/lib/theme";

// The gallery: every component of the library, by itself, in both themes.
export default function Gallery() {
  const [theme, setTheme] = useTheme();
  const next = { light: "dark", dark: "system", system: "light" } as const;
  return (
    <main className="mx-auto grid max-w-5xl gap-10 px-4 py-8">
      <header className="flex items-center gap-4">
        <Wordmark />
        <Badge variant="outline">design/ui</Badge>
        <Button
          variant="outline"
          size="sm"
          className="ml-auto"
          onClick={() => setTheme(next[theme])}
        >
          {theme === "dark" ? <Moon /> : <Sun />}
          Theme: {theme}
        </Button>
      </header>

      <Section title="Button">
        <div className="flex flex-wrap items-center gap-3">
          <Button>Default</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="outline">Outline</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="destructive">Destructive</Button>
          <Button variant="link">Link</Button>
          <Button size="sm">Small</Button>
          <Button disabled>Disabled</Button>
        </div>
      </Section>

      <Section title="Badge">
        <div className="flex flex-wrap items-center gap-3">
          <Badge>Default</Badge>
          <Badge variant="secondary">Secondary</Badge>
          <Badge variant="outline">Outline</Badge>
          <Badge variant="destructive">Destructive</Badge>
        </div>
      </Section>

      <Section title="Input and label">
        <div className="grid max-w-sm gap-2">
          <Label htmlFor="name">Project name</Label>
          <Input id="name" placeholder="hello-world" />
        </div>
      </Section>

      <Section title="Card">
        <div className="grid gap-4 md:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle>Overview</CardTitle>
              <CardDescription>What a card of the dashboard looks like.</CardDescription>
            </CardHeader>
            <CardContent className="text-sm text-muted-foreground">
              Release v12, two instances ready.
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Loading</CardTitle>
              <CardDescription>The same card while it waits.</CardDescription>
            </CardHeader>
            <CardContent className="grid gap-2">
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
            </CardContent>
          </Card>
        </div>
      </Section>

      <Section title="Alert">
        <Alert>
          <Info />
          <AlertTitle>The cluster is being upgraded</AlertTitle>
          <AlertDescription>Deploys wait until it ends.</AlertDescription>
        </Alert>
      </Section>

      <Section title="Tabs">
        <Tabs defaultValue="overview">
          <TabsList>
            <TabsTrigger value="overview">Overview</TabsTrigger>
            <TabsTrigger value="logs">Logs</TabsTrigger>
          </TabsList>
          <TabsContent value="overview" className="text-sm text-muted-foreground">
            The first tab.
          </TabsContent>
          <TabsContent value="logs" className="text-sm text-muted-foreground">
            The second tab.
          </TabsContent>
        </Tabs>
      </Section>

      <Section title="Table">
        <div className="rounded-xl border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Project</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Release</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow>
                <TableCell>Hello World</TableCell>
                <TableCell>
                  <Badge variant="outline">Running</Badge>
                </TableCell>
                <TableCell className="text-right font-mono text-xs">v12</TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </Section>

      <Section title="Dialog">
        <Dialog>
          <DialogTrigger asChild>
            <Button variant="outline">Open</Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Destroy the project?</DialogTitle>
              <DialogDescription>
                Its instances stop and its address is released.
              </DialogDescription>
            </DialogHeader>
          </DialogContent>
        </Dialog>
      </Section>
    </main>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="grid gap-3">
      <h2 className="text-sm font-medium text-muted-foreground">{title}</h2>
      {children}
    </section>
  );
}
