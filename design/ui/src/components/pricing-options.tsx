import * as React from "react";
import { Check, Minus } from "lucide-react";
import { cn } from "cn";

// The plans of an offer, side by side: what each is called and costs, what
// comes with it, and what to do about it. After Primer Brand's PricingOptions,
// with its parts given as props rather than looked for among the children.
//
// From one to four plans. They stand one under the other in a narrow room and
// side by side where each has room for its list - the room of the component,
// not of the window.
function PricingOptions({
  className,
  variant = "default",
  align = "start",
  children,
  ...props
}: React.ComponentProps<"div"> & {
  // `default` divides the plans with a line; `cards` puts each in a card.
  variant?: "default" | "cards";
  // `center` centres the words of every plan above its list.
  align?: "start" | "center";
}) {
  return (
    <div
      data-slot="pricing-options"
      data-variant={variant}
      data-align={align}
      className={cn("group/pricing @container/pricing", className)}
      {...props}
    >
      {/* The grid is inside the container: an element cannot answer a query
          about its own width. */}
      <div
        className={cn(
          // Side by side, the plans share five rows (the words, the actions,
          // the list, the usage, the fine print), so each part lines up
          // across the plans.
          "grid grid-cols-1 @2xl/pricing:auto-cols-fr @2xl/pricing:grid-flow-col @2xl/pricing:grid-rows-[auto_auto_1fr_auto_auto]",
          variant === "cards" ? "gap-6" : "gap-0",
        )}
      >
        {children}
      </div>
    </div>
  );
}

// One rate of a plan billed by usage: what is measured, and its price.
type UsageRate = {
  name: React.ReactNode;
  // Under the name, small: how it is measured ("reserved, while awake").
  note?: React.ReactNode;
  // "$0.02 / compute unit-hour", or "Included".
  value: React.ReactNode;
};

type Feature = {
  // What comes with the plan, or does not.
  children: React.ReactNode;
  // An `excluded` feature is shown, struck through in meaning: not in this plan.
  variant?: "included" | "excluded";
};

function PricingOption({
  className,
  as: Heading = "h3",
  label,
  heading,
  description,
  price,
  currencySymbol = "$",
  originalPrice,
  trailingText,
  featuresHeading = "What's included",
  features,
  usageHeading = "Usage",
  usage,
  actions,
  message,
  footnote,
  ...props
}: Omit<React.ComponentProps<"div">, "title"> & {
  // A section's own heading is `h2`, so a plan under it is `h3`.
  as?: "h2" | "h3" | "h4";
  // Over the heading: which plan is recommended, or who it is for.
  label?: React.ReactNode;
  heading: React.ReactNode;
  // One sentence: who the plan is for.
  description?: React.ReactNode;
  // The amount, without the currency: "0", "5", "21".
  price?: React.ReactNode;
  currencySymbol?: string;
  // The amount before a discount, struck through beside the price.
  originalPrice?: React.ReactNode;
  // After the price: "per month", "a month minimum, then usage".
  trailingText?: React.ReactNode;
  featuresHeading?: React.ReactNode;
  features?: Feature[];
  usageHeading?: React.ReactNode;
  // What the plan charges by usage, one line each. Plans side by side keep
  // the same lines in the same order, so they compare line by line.
  usage?: UsageRate[];
  // What to do about it: a button, or the page's own call to action.
  actions?: React.ReactNode;
  // Under the actions: a note about them ("Opened by hand during the beta").
  message?: React.ReactNode;
  // The fine print of the plan, at its foot.
  footnote?: React.ReactNode;
}) {
  return (
    <div
      data-slot="pricing-option"
      className={cn(
        "flex min-w-0 flex-col gap-6 py-8",
        "@2xl/pricing:row-span-5 @2xl/pricing:grid @2xl/pricing:grid-rows-subgrid @2xl/pricing:gap-y-6",
        // Default: a line between the plans - under each while they stack,
        // beside each once they stand side by side.
        "group-data-[variant=default]/pricing:border-b group-data-[variant=default]/pricing:last:border-b-0",
        "@2xl/pricing:group-data-[variant=default]/pricing:border-b-0 @2xl/pricing:group-data-[variant=default]/pricing:border-l @2xl/pricing:group-data-[variant=default]/pricing:px-8 @2xl/pricing:group-data-[variant=default]/pricing:first:border-l-0 @2xl/pricing:group-data-[variant=default]/pricing:first:pl-0 @2xl/pricing:group-data-[variant=default]/pricing:last:pr-0",
        // Cards: each plan in a card of its own.
        "group-data-[variant=cards]/pricing:rounded-xl group-data-[variant=cards]/pricing:bg-card group-data-[variant=cards]/pricing:px-6 group-data-[variant=cards]/pricing:text-card-foreground group-data-[variant=cards]/pricing:ring-1 group-data-[variant=cards]/pricing:ring-foreground/10",
        className,
      )}
      {...props}
    >
      <div className="grid content-start gap-3 group-data-[align=center]/pricing:justify-items-center group-data-[align=center]/pricing:text-center">
        {label && (
          <div data-slot="pricing-option-label" className="text-sm font-medium text-primary">
            {label}
          </div>
        )}
        <Heading data-slot="pricing-option-heading" className="font-heading text-xl font-semibold tracking-tight">
          {heading}
        </Heading>
        {description && (
          <p data-slot="pricing-option-description" className="text-sm text-muted-foreground">
            {description}
          </p>
        )}
        {price !== undefined && (
          <p
            data-slot="pricing-option-price"
            className="flex flex-wrap items-baseline gap-x-2 gap-y-1 group-data-[align=center]/pricing:justify-center"
          >
            {originalPrice !== undefined && (
              <s className="text-lg text-muted-foreground">
                <span className="sr-only">Was </span>
                {currencySymbol}
                {originalPrice}
              </s>
            )}
            <span className="font-heading text-4xl font-semibold tracking-tight">
              <span className="align-top text-2xl">{currencySymbol}</span>
              {price}
            </span>
            {trailingText && <span className="text-sm text-muted-foreground">{trailingText}</span>}
          </p>
        )}
      </div>

      {/* Every part is drawn, empty or not, so the rows of the plans hold
          when they stand side by side; while they stack, an empty one goes. */}
      <div
        data-slot="pricing-option-actions"
        className="grid content-start gap-2 @max-2xl/pricing:empty:hidden group-data-[align=center]/pricing:justify-items-center group-data-[align=center]/pricing:text-center"
      >
        {actions && (
          <div className="flex flex-wrap items-center gap-3 group-data-[align=center]/pricing:justify-center">{actions}</div>
        )}
        {actions && message && <p className="text-xs text-muted-foreground">{message}</p>}
      </div>

      <div data-slot="pricing-option-features" className="@max-2xl/pricing:empty:hidden">
        {features && features.length > 0 && (
          <div className="grid gap-3 border-t pt-6 text-sm">
            {featuresHeading && <p className="font-medium">{featuresHeading}</p>}
            <ul className="grid gap-2.5">
              {features.map((feature, i) => {
                const excluded = feature.variant === "excluded";
                return (
                  <li
                    key={i}
                    data-variant={excluded ? "excluded" : "included"}
                    className={cn("flex gap-2.5", excluded && "text-muted-foreground")}
                  >
                    {excluded ? (
                      <Minus aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
                    ) : (
                      <Check aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-primary" />
                    )}
                    <span>
                      {excluded && <span className="sr-only">Not included: </span>}
                      {feature.children}
                    </span>
                  </li>
                );
              })}
            </ul>
          </div>
        )}
      </div>

      <div data-slot="pricing-option-usage" className="@max-2xl/pricing:empty:hidden">
        {usage && usage.length > 0 && (
          <div className="grid gap-3 border-t pt-6 text-sm">
            {usageHeading && <p className="font-medium">{usageHeading}</p>}
            <dl className="grid gap-2.5">
              {usage.map((rate, i) => (
                <div key={i} className="flex items-baseline justify-between gap-4">
                  <dt className="grid">
                    <span className="text-muted-foreground">{rate.name}</span>
                    {rate.note && <span className="text-xs text-muted-foreground/70">{rate.note}</span>}
                  </dt>
                  <dd className="text-right font-medium tabular-nums">{rate.value}</dd>
                </div>
              ))}
            </dl>
          </div>
        )}
      </div>

      <div data-slot="pricing-option-footnote" className="self-end @max-2xl/pricing:empty:hidden">
        {footnote && <p className="text-xs text-muted-foreground">{footnote}</p>}
      </div>
    </div>
  );
}

export { PricingOptions, PricingOption };
export type { Feature as PricingFeature, UsageRate as PricingUsageRate };
