//! The Rust companion of the action-metadata generator.
//!
//! It is handed the path to an action's `src/main.rs` and prints WHAT READING
//! RUST ANSWERS: the doc comment that describes the action, the input schema
//! read off the payload type, every comment in the file, and the things the
//! schema could not say. Four members, and no fifth.
//!
//! IT STATES NOTHING ABOUT THE TAG VOCABULARY. `@tool`, `@shortdesc` and
//! `@usewhen` are claimed, validated and refused by the caller, which is the
//! program that writes `action.json`. That is why the comments travel out
//! whole and the description travels out RAW, with every annotation line still
//! where its author wrote it: the caller lifts its own lines out, once. A
//! second copy of the vocabulary here would be a rule two programs get to
//! disagree about, and both would exit zero while disagreeing. An `ai` member
//! is refused by the caller outright for the same reason, so this program does
//! not emit one.
//!
//! EXIT STATUS IS PART OF THE CONTRACT, not a detail of how this program
//! happens to end. A REFUSAL exits `2` and nothing else does. The caller acts
//! on the difference: `action.json` is generated wholesale from the source
//! beside it, so when the source is refused, the file still sitting on disk was
//! generated from an EARLIER source — it describes an action that no longer
//! exists and carries an exposure statement its author has since tried to
//! change. Nothing downstream can tell, because a well-formed file reads as
//! current. Only a refusal discards it; a generator that could not RUN has said
//! nothing about whether the file is true, and deleting every action's metadata
//! because a toolchain is absent turns one environment problem into a tree
//! nobody can build from.
//!
//! For the same reason this program must be BUILT and then run rather than
//! launched. A launcher collapses every non-zero status into one and prints the
//! real one as a line of text, so a refusal would reach the caller as an
//! ordinary failure and the stale file would survive it.

mod comments;
mod schema;
mod serde_attrs;
mod tags;

use std::path::Path;
use std::process::ExitCode;

use serde_json::Value;
use syn::{File, ItemFn, ItemStruct};

use crate::schema::{
    collect_declarations, description_of, doc_text, no_input_schema, Declarations, SchemaBuilder,
};
use crate::tags::{is_annotated, payload_structs_in, refuse, Refusal};

/// The status a refused source exits with, told apart from every other way this
/// program can fail.
const REFUSAL_EXIT_CODE: u8 = 2;

/// The name the payload type carries when its author does not point at one,
/// matching the TypeScript SDK, which reads an interface of this name.
const DEFAULT_PAYLOAD_STRUCT: &str = "Payload";

/// The function an action's contract is written on when the payload type does
/// not carry it.
const HANDLER_FN: &str = "handler";

/// What this generator states about one action.
#[derive(Debug)]
struct Extracted {
    /// The doc comment that describes the action, EXACTLY as its author wrote
    /// it. The caller takes its own annotation lines out.
    description: String,
    schema: Value,
    /// Every comment in the file, so the caller reads the exposure statement
    /// from wherever it was written. WITHOUT THESE THERE IS NO STATEMENT TO
    /// READ, and the caller fails the run rather than describing an action that
    /// declares `@tool` as an action that is not a tool.
    comments: Vec<String>,
    /// What the schema could not say. Handed to the caller rather than printed
    /// here, because the caller is what an author is running: a line this
    /// program writes to its own stderr is a line the caller never sees and
    /// cannot decide to show.
    gaps: Vec<String>,
}

fn main() -> ExitCode {
    let arguments: Vec<String> = std::env::args().skip(1).collect();

    let path = match arguments.first().map(String::as_str) {
        // `--` so the caller can hand over a path that begins with a dash
        // without this program reading it as a flag.
        Some("--") => arguments.get(1),
        Some(_) => arguments.first(),
        None => None,
    };

    let Some(path) = path else {
        eprintln!("Usage: extract_rustdoc [--] <main.rs>");

        return ExitCode::FAILURE;
    };

    let source = match std::fs::read_to_string(path) {
        Ok(source) => source,
        Err(error) => {
            eprintln!("Read error: {error}");

            return ExitCode::FAILURE;
        }
    };

    let file = match syn::parse_file(&source) {
        Ok(file) => file,
        Err(error) => {
            eprintln!("Parse error: {error}");

            return ExitCode::FAILURE;
        }
    };

    let action = action_name(path);

    match extract(&action, &source, &file) {
        Ok(extracted) => {
            // A gap is not a failure — the action is described, and the
            // description is correct as far as it goes — so it does not change
            // the status. It travels in the artifact, where the caller reports
            // it to the author.
            println!("{}", render(&extracted));

            ExitCode::SUCCESS
        }
        Err(refusal) => {
            eprintln!("{refusal}");

            ExitCode::from(REFUSAL_EXIT_CODE)
        }
    }
}

/// The action a source file belongs to, so a refusal names the action its
/// author is looking at rather than a path into its build layout.
///
/// A Rust action is a crate, and the crate's own name is the directory holding
/// `src`, not `src` itself.
fn action_name(path: &str) -> String {
    let path = Path::new(path);
    let Some(parent) = path.parent() else {
        return path.display().to_string();
    };

    let directory = match parent.file_name().and_then(|name| name.to_str()) {
        Some("src") => parent
            .parent()
            .and_then(|crate_root| crate_root.file_name()),
        other => other.map(std::ffi::OsStr::new),
    };

    match directory.and_then(|name| name.to_str()) {
        Some(name) if !name.is_empty() => name.to_string(),
        _ => path.display().to_string(),
    }
}

fn extract(action: &str, source: &str, file: &File) -> Result<Extracted, Refusal> {
    // EVERY COMMENT IN THE FILE, ONCE, WHEREVER ITS AUTHOR WROTE IT.
    //
    // Which comment supplies the DESCRIPTION is decided below, by where the
    // payload is declared. Handing over only the comment that happened to win
    // would drop a tag written in any of the others in silence — and a dropped
    // `@tool` is an action that quietly stops being callable, which is the
    // failure the annotation exists to make impossible.
    //
    // Position is not part of the rule either. A blank line between a block and
    // the declaration under it is not something an author sees, and it must not
    // decide whether the action is a tool.
    let comments = comments::comments_in(source);

    let declarations = collect_declarations(file);
    let payload = payload_struct(action, &comments, &declarations)?;
    let handler = handler_fn(&declarations);

    // THE PAYLOAD TYPE'S OWN DOC COMMENT DESCRIBES THE ACTION WHEN IT HAS ONE,
    // and the handler's otherwise.
    //
    // "Has one" is asked of the comment as written, because this program can no
    // longer ask what is left of it once the claimed lines come out — that is
    // the caller's vocabulary and this program does not hold it. An author who
    // documented the payload type documented the payload type.
    let description = payload
        .map(|item| description_of(&item.attrs))
        .filter(|described| !described.is_empty())
        .or_else(|| handler.map(|item| description_of(&item.attrs)))
        .unwrap_or_default();

    let mut builder = SchemaBuilder::new(action, &declarations);

    let schema = match payload {
        Some(item) => builder.payload_schema(item)?,
        None => no_input_schema(),
    };

    Ok(Extracted {
        description,
        schema,
        comments,
        gaps: builder.gaps().to_vec(),
    })
}

/// The struct an action declares its payload as.
///
/// `@Payload` names it where an author has one of their own; otherwise it is
/// the struct called `Payload`, which is the name the TypeScript SDK reads an
/// interface under.
///
/// A NAMED STRUCT THAT IS NOT THERE IS REFUSED. Falling back to the no-input
/// schema would advertise a tool that takes nothing, and a model calling it
/// with nothing is a call the action rejects for reasons nobody can see.
fn payload_struct<'a>(
    action: &str,
    comments: &[String],
    declarations: &Declarations<'a>,
) -> Result<Option<&'a ItemStruct>, Refusal> {
    let pointed_at: Vec<String> = comments
        .iter()
        .flat_map(|comment| payload_structs_in(comment))
        .collect();

    if pointed_at.len() > 1 {
        return Err(refuse(
            action,
            &format!(
                "points at {} payload types with @Payload, and an action has one",
                pointed_at.len()
            ),
            &[],
        ));
    }

    let named = pointed_at.first();
    let wanted = named.map_or(DEFAULT_PAYLOAD_STRUCT, String::as_str);

    // A NAME THIS FILE GIVES TO TWO DECLARATIONS RESOLVES TO NEITHER. The
    // schema would be read off whichever one the walk reached last, which is
    // source order rather than scope.
    if declarations.is_ambiguous(wanted) {
        return Err(refuse(
            action,
            &format!(
                "declares `{wanted}` more than once, so which one the payload is read from is \
                 decided by the order the declarations appear in rather than by what the handler \
                 deserializes"
            ),
            &[],
        ));
    }

    match declarations.structs.get(wanted).copied() {
        Some(item) => Ok(Some(item)),
        None if named.is_some() => Err(refuse(
            action,
            &format!("names `{wanted}` as its payload, and no such struct is declared here"),
            &[],
        )),
        None => Ok(None),
    }
}

/// The function an action's contract is written on.
///
/// The conventional name first, then a function whose doc comment carries an
/// annotation of any kind — because an author who annotated a function has said
/// which function describes the action, whatever they called it.
///
/// The second rule asks about the SHAPE of a line rather than about a name.
/// Naming the exposure tags here would be this program claiming the vocabulary
/// it hands to its caller, and it would answer no differently for any source
/// that has a `handler` — which every action generated from a template does.
fn handler_fn<'a>(declarations: &Declarations<'a>) -> Option<&'a ItemFn> {
    if let Some(named) = declarations
        .functions
        .iter()
        .find(|item| item.sig.ident == HANDLER_FN)
        .copied()
    {
        return Some(named);
    }

    declarations
        .functions
        .iter()
        .find(|item| is_annotated(&doc_text(&item.attrs)))
        .copied()
}

/// FOUR MEMBERS, ALWAYS, AND NO FIFTH.
///
/// `description`, `schema`, `comments`, `gaps`. Each is stated even when it is
/// empty: an absent `comments` is how a caller ends up reading a file with no
/// exposure statement in it as an action that never claimed to be a tool, and
/// the caller fails the run on its absence rather than guessing. `gaps` is
/// stated for the same reason, so a caller never has to tell "found none" apart
/// from "did not look".
///
/// THERE IS NO `ai` MEMBER. The vocabulary belongs to the caller, and the
/// caller refuses an answer carrying one outright — a second copy of a rule is
/// how two programs describing one action drift apart while both exit zero.
///
/// The member order is the file format rather than a style choice. Serialising
/// through a sorted map would reorder it, so the members are written one by one
/// and only the schema — whose keys ARE sorted, as the other generators sort
/// them — is serialised whole.
fn render(extracted: &Extracted) -> String {
    let members = vec![
        (
            "description",
            Value::String(extracted.description.clone()).to_string(),
        ),
        ("schema", extracted.schema.to_string()),
        ("comments", strings(&extracted.comments)),
        ("gaps", strings(&extracted.gaps)),
    ];

    object(&members)
}

/// A JSON array of strings, kept in the order it was collected.
fn strings(values: &[String]) -> String {
    Value::Array(
        values
            .iter()
            .map(|value| Value::String(value.clone()))
            .collect(),
    )
    .to_string()
}

/// A JSON object whose members keep the order they were written in.
///
/// Each value arrives already serialised, so what this adds is the order and
/// nothing else.
fn object(members: &[(&str, String)]) -> String {
    let body: Vec<String> = members
        .iter()
        .map(|(name, value)| format!("{}:{value}", Value::String((*name).to_string())))
        .collect();

    format!("{{{}}}", body.join(","))
}

#[cfg(test)]
mod tests {
    use serde_json::Value;

    use super::{action_name, extract, render, Extracted};
    use crate::tags::Refusal;

    fn describe(source: &str) -> Extracted {
        let file = syn::parse_file(source).expect("the source parses");

        extract("invoices", source, &file).expect("the source is described")
    }

    fn refusal(source: &str) -> Refusal {
        let file = syn::parse_file(source).expect("the source parses");

        extract("invoices", source, &file).expect_err("the source is refused")
    }

    fn json(source: &str) -> Value {
        serde_json::from_str(&render(&describe(source))).expect("the output is JSON")
    }

    fn property<'a>(document: &'a Value, name: &str) -> &'a Value {
        &document["schema"]["properties"][name]
    }

    fn required(document: &Value) -> Vec<String> {
        document["schema"]["required"]
            .as_array()
            .map(|values| {
                values
                    .iter()
                    .map(|value| value.as_str().unwrap_or_default().to_string())
                    .collect()
            })
            .unwrap_or_default()
    }

    const TOOL: &str = "\
/// The full contract, in prose.
///
/// @tool
/// @short_desc Totals a customer's open invoices.
/// @when_use The user asks what a customer owes.
fn handler(request: Request<Payload>) -> Result<Output, Error> { todo!() }
";

    #[test]
    fn a_field_that_is_not_an_option_is_required() {
        let source = format!(
            "struct Payload {{\n    /// The customer.\n    customer_id: String,\n}}\n{TOOL}"
        );
        let document = json(&source);

        assert_eq!(property(&document, "customer_id")["type"], "string");
        assert_eq!(
            property(&document, "customer_id")["description"],
            "The customer."
        );
        assert_eq!(required(&document), vec!["customer_id".to_string()]);
    }

    #[test]
    fn an_option_is_optional_rather_than_nullable() {
        let source = format!("struct Payload {{\n    limit: Option<i64>,\n}}\n{TOOL}");
        let document = json(&source);

        assert_eq!(property(&document, "limit")["type"], "integer");
        assert!(property(&document, "limit").get("anyOf").is_none());
        assert!(required(&document).is_empty());
    }

    #[test]
    fn a_serde_default_makes_a_member_optional_without_changing_its_type() {
        let source = format!(
            "struct Payload {{\n    #[serde(default)]\n    page: i32,\n    name: String,\n}}\n{TOOL}"
        );
        let document = json(&source);

        assert_eq!(property(&document, "page")["type"], "integer");
        assert_eq!(required(&document), vec!["name".to_string()]);
    }

    #[test]
    fn serde_rename_names_the_property() {
        let source = format!(
            "struct Payload {{\n    #[serde(rename = \"customerId\")]\n    customer_id: String,\n}}\n{TOOL}"
        );
        let document = json(&source);

        assert_eq!(property(&document, "customerId")["type"], "string");
        assert!(document["schema"]["properties"]
            .get("customer_id")
            .is_none());
        assert_eq!(required(&document), vec!["customerId".to_string()]);
    }

    #[test]
    fn serde_rename_all_names_every_property() {
        let source = format!(
            "#[serde(rename_all = \"camelCase\")]\nstruct Payload {{\n    customer_id: String,\n    invoice_number: String,\n}}\n{TOOL}"
        );
        let document = json(&source);

        assert_eq!(property(&document, "customerId")["type"], "string");
        assert_eq!(property(&document, "invoiceNumber")["type"], "string");
        assert_eq!(
            required(&document),
            vec!["customerId".to_string(), "invoiceNumber".to_string()]
        );
    }

    #[test]
    fn serde_skip_takes_a_member_out_of_the_schema() {
        let source = format!(
            "struct Payload {{\n    #[serde(skip)]\n    internal: String,\n    #[serde(skip_deserializing)]\n    computed: String,\n    kept: String,\n}}\n{TOOL}"
        );
        let document = json(&source);

        let properties = document["schema"]["properties"]
            .as_object()
            .expect("an object");

        assert_eq!(properties.len(), 1);
        assert!(properties.contains_key("kept"));
        assert_eq!(required(&document), vec!["kept".to_string()]);
    }

    #[test]
    fn a_vec_is_an_array_with_items() {
        let source = format!("struct Payload {{\n    tags: Vec<String>,\n}}\n{TOOL}");
        let document = json(&source);

        assert_eq!(property(&document, "tags")["type"], "array");
        assert_eq!(property(&document, "tags")["items"]["type"], "string");
    }

    #[test]
    fn a_map_is_an_object_with_additional_properties() {
        let source = format!(
            "struct Payload {{\n    counts: HashMap<String, i64>,\n    open: BTreeMap<String, Value>,\n}}\n{TOOL}"
        );
        let document = json(&source);

        assert_eq!(property(&document, "counts")["type"], "object");
        assert_eq!(
            property(&document, "counts")["additionalProperties"]["type"],
            "integer"
        );
        assert_eq!(property(&document, "open")["additionalProperties"], true);
    }

    #[test]
    fn a_nested_struct_is_an_inline_object_with_its_own_required_list() {
        let source = format!(
            "struct Payload {{\n    /// Where to send it.\n    address: Address,\n}}\n\nstruct Address {{\n    /// The street.\n    street: String,\n    unit: Option<String>,\n}}\n{TOOL}"
        );
        let document = json(&source);
        let address = property(&document, "address");

        assert_eq!(address["type"], "object");
        assert_eq!(address["description"], "Where to send it.");
        assert_eq!(address["properties"]["street"]["type"], "string");
        assert_eq!(
            address["properties"]["street"]["description"],
            "The street."
        );
        assert_eq!(address["properties"]["unit"]["type"], "string");
        assert_eq!(
            address["required"].as_array().expect("a list"),
            &vec![Value::String("street".to_string())]
        );
    }

    #[test]
    fn a_fieldless_enum_is_a_string_with_an_enum() {
        let source = format!(
            "struct Payload {{\n    status: Status,\n}}\n\n#[serde(rename_all = \"snake_case\")]\nenum Status {{\n    OpenOnly,\n    Paid,\n}}\n{TOOL}"
        );
        let document = json(&source);

        assert_eq!(property(&document, "status")["type"], "string");
        assert_eq!(
            property(&document, "status")["enum"],
            Value::Array(vec![
                Value::String("open_only".to_string()),
                Value::String("paid".to_string()),
            ])
        );
    }

    #[test]
    fn the_description_is_raw_and_keeps_every_tag_line_where_it_was_written() {
        let source = format!("struct Payload {{\n    customer_id: String,\n}}\n{TOOL}");
        let document = json(&source);

        // The caller lifts these lines out ONCE, with the vocabulary it owns.
        // Taking them out here as well is not merely redundant: it makes the
        // rule about which lines go two programs' business, and the day they
        // disagree the description either ships a `@tool` a model reads as
        // prose or loses a sentence its author wrote.
        assert_eq!(
            document["description"],
            "The full contract, in prose.\n\n@tool\n@short_desc Totals a customer's open \
             invoices.\n@when_use The user asks what a customer owes."
        );
    }

    #[test]
    fn no_source_gets_an_ai_member_however_it_is_annotated() {
        // THE VOCABULARY IS THE CALLER'S. Its own reader refuses an answer
        // carrying an `ai` member outright, so emitting one for any source at
        // all would fail every Rust action in the tree.
        for source in [
            "/// Prose only.\nfn handler() {}\n",
            "/// @tool\n/// @shortdesc Totals invoices.\nfn handler() {}\n",
            "/// @tool true\n/// @short_desc Totals invoices.\nfn handler() {}\n",
        ] {
            let document = json(source);

            assert!(document.get("ai").is_none(), "{document}");
        }
    }

    #[test]
    fn a_tag_the_caller_would_refuse_is_described_rather_than_refused_here() {
        // A value on the modifier tag, a retired name that is also one edit
        // from `@shortdesc`, and a second retired name: every one of them is
        // refused, and by the caller. This program hands the lines over and
        // says nothing, because a rule with two homes is a rule two programs
        // get to answer differently.
        let source = "\
/// @tool true
/// @short_desc Totals invoices.
/// @effects read
fn handler() {}
";
        let document = json(source);

        assert!(document.get("ai").is_none());
        assert_eq!(
            document["comments"],
            Value::Array(vec![
                Value::String(" @tool true".to_string()),
                Value::String(" @short_desc Totals invoices.".to_string()),
                Value::String(" @effects read".to_string()),
            ])
        );
    }

    #[test]
    fn an_action_that_declares_no_payload_accepts_nothing() {
        let source = "\
/// Prose only.
fn handler() {}
";
        let document = json(source);

        assert_eq!(document["description"], "Prose only.");
        assert_eq!(document["schema"]["additionalProperties"], false);
        assert_eq!(document["schema"]["type"], "object");
    }

    #[test]
    fn a_flattened_member_puts_its_own_members_in_its_place() {
        let source = format!(
            "struct Payload {{\n    id: String,\n    #[serde(flatten)]\n    page: Paging,\n}}\n\nstruct Paging {{\n    limit: i64,\n    cursor: Option<String>,\n}}\n{TOOL}"
        );
        let document = json(&source);
        let properties = document["schema"]["properties"]
            .as_object()
            .expect("an object");

        assert!(properties.get("page").is_none(), "{document}");
        assert_eq!(property(&document, "limit")["type"], "integer");
        assert_eq!(property(&document, "cursor")["type"], "string");
        assert_eq!(
            required(&document),
            vec!["id".to_string(), "limit".to_string()]
        );
    }

    #[test]
    fn a_member_doc_comment_writing_minimum_states_no_constraint() {
        let source = format!(
            "struct Payload {{\n    /// How many rows to read.\n    /// @minimum 1\n    limit: i64,\n}}\n{TOOL}"
        );
        let extracted = describe(&source);
        let document = json(&source);
        let limit = property(&document, "limit");

        assert_eq!(limit["type"], "integer");
        assert!(limit.get("minimum").is_none(), "{limit}");
        assert_eq!(extracted.gaps.len(), 1, "{:?}", extracted.gaps);
        assert!(
            extracted.gaps[0].contains("@minimum"),
            "{:?}",
            extracted.gaps
        );
        assert!(
            extracted.gaps[0].contains("`limit`"),
            "{:?}",
            extracted.gaps
        );
        assert_eq!(
            limit["description"], "How many rows to read.\n@minimum 1",
            "the line stays exactly where its author wrote it"
        );
    }

    #[test]
    fn the_payload_annotation_points_at_a_differently_named_struct() {
        let source = "\
/// The invoice query.
struct InvoiceQuery {
    customer_id: String,
}

/// Handler prose.
///
/// @Payload InvoiceQuery
/// @tool
/// @short_desc Totals invoices.
fn handler() {}
";
        let document = json(source);

        assert_eq!(document["description"], "The invoice query.");
        assert_eq!(property(&document, "customer_id")["type"], "string");
    }

    #[test]
    fn every_comment_travels_whatever_syntax_carries_it_and_wherever_it_sits() {
        // WHERE A TAG IS WRITTEN IS NOT PART OF THE RULE, and this program is
        // the only thing that can honour that: the caller never sees the
        // source. A comment dropped here is a `@tool` the caller cannot read,
        // and an unheard `@tool` reads exactly like an action that never
        // claimed to be one.
        let source = "\
// @tool

/// Prose.
struct Payload {
    customer_id: String,
}

fn handler() {}

/*
 * @short_desc Totals invoices.
 */
fn main() {}
";
        let document = json(source);
        let comments = document["comments"].as_array().expect("a list");
        let text: Vec<&str> = comments
            .iter()
            .map(|comment| comment.as_str().unwrap_or_default())
            .collect();

        assert!(
            text.iter().any(|comment| comment.contains("@tool")),
            "{text:?}"
        );
        assert!(
            text.iter().any(|comment| comment.contains("@short_desc")),
            "{text:?}"
        );
        assert!(
            text.iter().any(|comment| comment.contains("Prose.")),
            "{text:?}"
        );
    }

    #[test]
    fn the_handler_describes_the_action_when_the_payload_says_nothing() {
        let source = format!("struct Payload {{\n    customer_id: String,\n}}\n{TOOL}");

        assert!(
            describe(&source)
                .description
                .starts_with("The full contract, in prose."),
            "the handler's prose describes an action whose payload type says nothing"
        );
    }

    #[test]
    fn the_member_order_of_the_output_is_the_file_format() {
        let source = format!("struct Payload {{\n    customer_id: String,\n}}\n{TOOL}");
        let printed = render(&describe(&source));

        let description = printed.find("\"description\"").expect("a description");
        let schema = printed.find("\"schema\"").expect("a schema");
        let comments = printed.find("\"comments\"").expect("a comments list");
        let gaps = printed.find("\"gaps\"").expect("a gaps list");

        assert!(description < schema, "{printed}");
        assert!(schema < comments && comments < gaps, "{printed}");
        assert!(!printed.contains("\"ai\""), "{printed}");
    }

    #[test]
    fn an_empty_list_is_stated_rather_than_left_out() {
        // A caller must never have to tell "found none" apart from "did not
        // look". It fails the run on a missing `comments`, and it would print
        // nothing at all for a missing `gaps` — silently, from a run that
        // exited zero.
        let document = json("/// Prose only.\nfn handler() {}\n");

        assert_eq!(document["gaps"], Value::Array(Vec::new()));
        assert!(document["comments"].is_array(), "{document}");
    }

    #[test]
    fn the_action_is_named_by_its_crate_rather_than_by_src() {
        assert_eq!(
            action_name("simple-apps/app/actions/total-due/src/main.rs"),
            "total-due"
        );
        assert_eq!(action_name("actions/total-due/main.rs"), "total-due");
    }

    #[test]
    fn a_payload_annotation_naming_nothing_is_refused() {
        let source = "\
/// @Payload Missing
/// @tool
/// @short_desc Totals invoices.
fn handler() {}
";
        let refused = refusal(source);

        assert!(refused.message.contains("`Missing`"), "{refused}");
    }

    #[test]
    fn two_payload_annotations_are_refused_here_because_nothing_else_refuses_them() {
        // THE ONLY HOME FOR THIS ONE. The caller reads `@Payload` out of the
        // statement and does not count it — it is a directive to this program
        // rather than a claim about exposure — so a second one would otherwise
        // pass both readers, and the schema would be read off whichever struct
        // this program happened to take first.
        let source = "\
/// @Payload InvoiceQuery
/// @Payload OrderQuery
/// @tool
/// @short_desc Totals invoices.
fn handler() {}
";
        let refused = refusal(source);

        assert!(refused.message.contains("@Payload"), "{refused}");
        assert!(refused.message.contains("an action has one"), "{refused}");
    }

    #[test]
    fn a_member_typed_by_something_undescribable_is_refused_rather_than_opened_up() {
        let source = format!("struct Payload {{\n    at: DateTime<Utc>,\n}}\n{TOOL}");
        let refused = refusal(&source);

        assert!(refused.message.contains("DateTime"), "{refused}");
    }
}
