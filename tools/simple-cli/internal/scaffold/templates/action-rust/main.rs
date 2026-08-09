//! `{{.ActionName}}`: a starting point you can run before you change anything.
//!
//! Everything an action needs is here and nothing else is: a payload with its
//! constraints, a handler with the three tags that describe it, `main`, and the
//! tests. Replace the greeting with what this action actually does — the shape
//! around it is the shape a finished action keeps.

use simpleplatform_sdk::prelude::*;

// THE PAYLOAD TYPE'S DOC COMMENT IS THE ACTION'S DESCRIPTION. It is read from
// here and not from the handler, because the payload is what a caller is being
// asked to fill in — so the description and the schema come off ONE declaration
// rather than two that can drift. Replace the doc line below with what this
// action does; this block is an ordinary comment and never reaches it.
//
// THE NAME IS PART OF THE CONTRACT. The payload is found by being called
// `Payload`, the same name an action written in TypeScript declares its
// interface under. A payload type called anything else is not found at all, and
// nothing says so: what ships is an action advertising that it takes no input,
// beside a handler that requires this field. A model then calls it with nothing
// and the action refuses for a reason nobody can see. Rename this type only
// together with a `@Payload` line naming what it is now called.
//
// The doc comment on a member is that member's description, its type is what
// says whether it is required, and `#[simple(…)]` carries the bounds — so the
// advertised schema and the code that reads it cannot drift apart, because they
// are the same lines.
/// {{.DisplayName}}: greet whoever the caller names.
{{- if .Description}}
///
/// {{.Description}}
{{- end}}
#[derive(Deserialize, Schema)]
struct Payload {
    /// Who to greet.
    #[simple(length(min = 1, max = 100), example = "World")]
    name: String,
}

/// What this action answers with.
#[derive(Debug, Serialize)]
struct Output {
    message: String,
}

fn main() {
    simple::run(handler)
}

/// The three tags below are what a caller reads when choosing between tools.
///
/// They are read from every comment in this file rather than from one of them,
/// so they sit with the handler they describe and the description stays with
/// the payload. A tag written anywhere here counts, and a tag written twice is
/// still written twice.
///
/// @tool
/// @shortdesc Greet whoever the caller names, and answer with the greeting.
/// @usewhen A greeting is wanted for a named person.
fn handler(request: Request<Payload>) -> Result<Output, Error> {
    let name = request.data.name.trim();

    // Refuse with a message a caller can act on and a hint saying what to do
    // next. A refusal is part of the contract, not an afterthought.
    if name.is_empty() {
        return Err(Error::invalid("'name' must not be blank.")
            .hint("Pass the name of whoever should be greeted."));
    }

    Ok(Output {
        message: format!("Hello, {name}!"),
    })
}

#[cfg(test)]
mod tests {
    use simpleplatform_sdk::testing;

    use super::*;

    // These run on this machine, under `simple test`, with no wasm and no
    // emulator: `testing::install` stands in for the host, so the handler is
    // called directly and answers in the same types production hands it.

    #[test]
    fn it_greets_the_name_it_was_given() {
        let session = testing::install(|_name, _params| Ok(json!(null)));

        let output = handler(Request::new(Payload {
            name: " World ".into(),
        }))
        .unwrap();

        assert_eq!(output.message, "Hello, World!");
        assert!(session.calls().is_empty());
    }

    #[test]
    fn a_blank_name_is_refused() {
        let _session = testing::install(|_name, _params| Ok(json!(null)));

        let error = handler(Request::new(Payload { name: "   ".into() })).unwrap_err();

        assert_eq!(error.code().as_str(), "INVALID_TOOL_INPUT");
        assert!(error.message().contains("name"));
    }

    #[test]
    fn the_whole_run_reports_one_readable_envelope() {
        // `simple::run` works under a session too, so a test can assert the
        // exact document the platform reads at the end of a run.
        let session = testing::install(|_name, _params| Ok(json!(null)))
            .with_request(json!({ "name": "World" }));

        simple::run(handler);

        let done = session.done().unwrap();

        assert_eq!(done["ok"], json!(true));
        assert_eq!(done["errors"], json!([]));
        assert_eq!(done["data"]["message"], json!("Hello, World!"));
    }
}
