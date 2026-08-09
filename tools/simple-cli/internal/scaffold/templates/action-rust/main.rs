//! `{{.ActionName}}`: a starting point you can run before you change anything.
//!
//! Everything an action needs is here and nothing else is: a payload with its
//! constraints, a handler with the three tags that describe it, `main`, and the
//! tests. Replace the greeting with what this action actually does — the shape
//! around it is the shape a finished action keeps.

use simpleplatform_sdk::prelude::*;

/// What the caller sends this action.
///
/// The doc comment on a member is its description, its type is what says
/// whether it is required, and `#[simple(…)]` carries the bounds — so the
/// advertised schema and the code that reads it cannot drift apart, because
/// they are the same lines.
#[derive(Deserialize, Schema)]
struct Input {
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

/// {{.DisplayName}}: greet whoever the caller names.
{{- if .Description}}
///
/// {{.Description}}
{{- end}}
///
/// The prose above is the full description of this action, exactly as written,
/// so replace it with what this action does once it does something. The three
/// tags below are what a caller reads when choosing between tools.
///
/// @tool
/// @shortdesc Greet whoever the caller names, and answer with the greeting.
/// @usewhen A greeting is wanted for a named person.
fn handler(request: Request<Input>) -> Result<Output, Error> {
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

        let output = handler(Request::new(Input {
            name: " World ".into(),
        }))
        .unwrap();

        assert_eq!(output.message, "Hello, World!");
        assert!(session.calls().is_empty());
    }

    #[test]
    fn a_blank_name_is_refused() {
        let _session = testing::install(|_name, _params| Ok(json!(null)));

        let error = handler(Request::new(Input { name: "   ".into() })).unwrap_err();

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
