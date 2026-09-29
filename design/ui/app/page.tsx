import Link from "next/link";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@shpyrd/ui/components/card";
import { PageHeading } from "@shpyrd/ui/components/page-heading";
import { catalog, href } from "./catalog";

// The way in: the four categories, and what each holds.
export default function Overview() {
  return (
    <>
      <PageHeading
        title="Overview"
        description="Every component of the library, by itself, in both themes."
      />
      <div className="grid gap-4 @3xl/page-layout:grid-cols-2">
        {catalog.map((category) => (
          <Card key={category.slug}>
            <CardHeader>
              <CardTitle>{category.title}</CardTitle>
              <CardDescription>{category.description}</CardDescription>
            </CardHeader>
            <CardContent>
              <ul className="flex flex-wrap gap-x-4 gap-y-1">
                {category.pages.map((page) => (
                  <li key={page.slug}>
                    <Link
                      href={href(category, page)}
                      className="text-primary underline-offset-4 hover:underline"
                    >
                      {page.title}
                    </Link>
                  </li>
                ))}
              </ul>
            </CardContent>
          </Card>
        ))}
      </div>
    </>
  );
}
