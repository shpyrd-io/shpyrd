import { copy as salesCopy } from "@shpyrd/content/site/contact/sales.mdx";
import { copy as enterpriseCopy } from "@shpyrd/content/site/contact/enterprise.mdx";
import { copy as formCopy } from "@shpyrd/content/site/contact/form.mdx";
import type { SalesContactCopy, EnterpriseContactCopy, ContactFormCopy } from "./contracts";
import { pricing } from "./pricing";
import { interpolate } from "./interpolate";

export type Choice = { value: string; label: string };
export type ContactField = {
  name: string; label: string; type: "text" | "email" | "select" | "textarea";
  required?: boolean; choices?: Choice[]; autoComplete?: string;
};
export type ContactPage = {
  title: string; description: string; submit: string; fields: ContactField[];
  beside: { heading: string; items: string[] }; after: { heading: string; items: string[] };
};
const copy = formCopy as ContactFormCopy;
export const { privacy, technical, failed, errors } = copy;
export const thanks = (email: string) => interpolate(copy.thanks, { email });
const label = (name: string) => {
  const field = copy.fields.find((item) => item.name === name);
  if (!field) throw new Error(`Missing contact field label: ${name}`);
  return field.label;
};
const field = (name: string, type: ContactField["type"], autoComplete?: string): ContactField => ({ name, type, autoComplete, label: label(name), required: true });
const person = [field("firstName", "text", "given-name"), field("lastName", "text", "family-name"), field("email", "email", "email")];
const variables = { replyFirst: copy.replyFirst };
export const sales: ContactPage = {
  ...interpolate((salesCopy as SalesContactCopy).page, variables),
  fields: [...person, field("message", "textarea")],
};
const enterprisePage = interpolate((enterpriseCopy as EnterpriseContactCopy).page, variables);
export const enterprise: ContactPage = {
  ...enterprisePage,
  fields: [...person, field("company", "text", "organization"), field("jobTitle", "text", "organization-title"),
    { ...field("size", "select"), choices: copy.companySizes }, field("message", "textarea")],
  beside: { ...enterprisePage.beside, items: pricing.enterprise.features },
};
