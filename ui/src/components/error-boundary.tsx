import { Component, type ReactNode } from "react";
import { useLocation } from "react-router-dom";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

type Props = {
  children: ReactNode;
  /** What the boundary protects, e.g. "Economics card". Shown in the message. */
  what?: string;
  /** Changing this resets the boundary (a route change, a card's query key). */
  resetKey?: string;
};

type State = { error: Error | null };

/**
 * ErrorBoundary keeps one broken component from blanking the whole page.
 * A card that throws while rendering shows a small inline message with a
 * retry button; the rest of the page keeps working.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidUpdate(prev: Props) {
    if (prev.resetKey !== this.props.resetKey && this.state.error) {
      this.setState({ error: null });
    }
  }

  componentDidCatch(error: Error) {
    console.error("shpyrd ui:", this.props.what ?? "component", error);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <Card className="border-destructive/40">
        <CardContent className="flex flex-wrap items-center justify-between gap-3 py-4 text-sm">
          <div>
            <p className="font-medium">
              {this.props.what ?? "This section"} could not be shown.
            </p>
            <p className="font-mono text-xs text-muted-foreground">
              {this.state.error.message}
            </p>
          </div>
          <Button
            size="sm"
            variant="outline"
            onClick={() => this.setState({ error: null })}
          >
            Try again
          </Button>
        </CardContent>
      </Card>
    );
  }
}

/** PageBoundary resets on every navigation so a broken page never sticks. */
export function PageBoundary({ children }: { children: ReactNode }) {
  const loc = useLocation();
  return (
    <ErrorBoundary what="This page" resetKey={loc.pathname}>
      {children}
    </ErrorBoundary>
  );
}
