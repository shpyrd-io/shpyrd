import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { Conversation, ConversationMessage } from "./conversation";

describe("Conversation", () => {
  it("lists the turns in order, each with who said it", () => {
    render(
      <Conversation>
        <ConversationMessage from="person" author="You">
          Share it with Finance.
        </ConversationMessage>
        <ConversationMessage from="agent" author="Your agent">
          Done.
        </ConversationMessage>
      </Conversation>,
    );
    const turns = screen.getAllByRole("listitem");
    expect(turns).toHaveLength(2);
    expect(turns[0].textContent).toContain("You");
    expect(turns[0].textContent).toContain("Share it with Finance.");
    expect(turns[1].getAttribute("data-from")).toBe("agent");
  });

  it("shows what the agent did before it answered, done unless it says otherwise", () => {
    const { container } = render(
      <Conversation>
        <ConversationMessage
          from="agent"
          author="Your agent"
          steps={[{ label: "Deployed release 1" }, { label: "Sharing", status: "pending" }]}
        >
          Almost.
        </ConversationMessage>
      </Conversation>,
    );
    expect(screen.getByText("Deployed release 1")).toBeTruthy();
    expect(screen.getByText("Sharing")).toBeTruthy();
    expect(container.querySelector("[data-slot=conversation-steps]")).toBeTruthy();
  });

  it("has no steps where none are given", () => {
    const { container } = render(
      <Conversation>
        <ConversationMessage from="person" author="You">
          Hello
        </ConversationMessage>
      </Conversation>,
    );
    expect(container.querySelector("[data-slot=conversation-steps]")).toBeNull();
  });
});
