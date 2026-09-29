import { Alert, AlertDescription, AlertTitle } from "@shpyrd/ui/components/alert";
import { IDE } from "@shpyrd/ui/components/ide";
import { InlineCode } from "@shpyrd/ui/components/inline-code";
import { Prose } from "@shpyrd/ui/components/prose";
import { Info } from "lucide-react";
import { Section } from "../../section";
import { deploy, manifest, server } from "../../code-samples";

const html = `
  <h2>From HTML</h2>
  <p>A text that comes as <strong>HTML</strong>, from a document that was written elsewhere, takes the same look. It has <a href="#prose">links</a>, <em>emphasis</em> and <code>code</code>.</p>
  <ul>
    <li>It is shown as it comes.</li>
    <li>What is not trusted has to be cleaned before.</li>
  </ul>
`;

export default function Page() {
  return (
    <>
      <Section title="A document, with components of the library inside">
        <Prose>
          <h1>Deploy your first project</h1>
          <p>
            A project is an application and everything it needs to run: its instances, its
            address and its releases. This page takes one from a folder on your machine to{" "}
            <a href="#prose">an address anyone can open</a>.
          </p>

          <h2>Before you start</h2>
          <p>You need two things:</p>
          <ul>
            <li>
              The command line, <InlineCode>shpyrd</InlineCode>, signed in to your workspace.
            </li>
            <li>
              An application that reads its port from <InlineCode>PORT</InlineCode>.
              <ul>
                <li>Node, Go, Python and Ruby are found by themselves.</li>
                <li>Anything else runs from a Dockerfile.</li>
              </ul>
            </li>
          </ul>

          <Alert>
            <Info />
            <AlertTitle>The address cannot be changed later</AlertTitle>
            <AlertDescription>It is made from the name the project is born with.</AlertDescription>
          </Alert>

          <h2>Say what runs</h2>
          <p>
            The file <InlineCode>shpyrd.yaml</InlineCode> names the processes of the project and
            the size of each. The server only has to answer on the port it is given:
          </p>

          <IDE
            files={[
              { name: "shpyrd.yaml", code: manifest },
              { name: "server.js", code: server },
            ]}
          />

          <h3>Then deploy</h3>
          <ol>
            <li>Sign in.</li>
            <li>Deploy what is in the folder.</li>
            <li>Follow the logs of the process.</li>
          </ol>

          <IDE code={deploy} language="sh" showLineNumbers={false} />

          <blockquote>
            <p>
              A release is what was built from one deploy. Going back to the one before takes
              as long as starting its instances.
            </p>
          </blockquote>

          <h3>What each size gives</h3>
          <table>
            <caption>The sizes of a shared instance.</caption>
            <thead>
              <tr>
                <th>Size</th>
                <th>CPU</th>
                <th>Memory</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>
                  <code>shared-s</code>
                </td>
                <td>0.5 cores</td>
                <td>512 MiB</td>
              </tr>
              <tr>
                <td>
                  <code>shared-m</code>
                </td>
                <td>1 core</td>
                <td>1 GiB</td>
              </tr>
              <tr>
                <td>
                  <code>shared-l</code>
                </td>
                <td>2 cores</td>
                <td>2 GiB</td>
              </tr>
            </tbody>
          </table>

          <h4>Code by itself</h4>
          <p>Code that is not given to an IDE is drawn plainly:</p>
          <pre>
            <code>{"shpyrd deploy --project hello-world"}</code>
          </pre>

          <hr />

          <h5>A heading of the fifth level</h5>
          <p>For what is under what is under something.</p>
          <h6>And of the sixth</h6>
          <p>The last there is.</p>
        </Prose>
      </Section>
      <Section title="From HTML">
        <Prose html={html} />
      </Section>
      <Section title="As wide as its place">
        <Prose fullWidth>
          <p>
            Without a width of its own, a line is as long as the place it is in. That is for a
            text that already sits in a column of a good width: a line that is too long is hard
            to follow from its end to the start of the next.
          </p>
        </Prose>
      </Section>
    </>
  );
}
