//! THE ANNOTATIONS THIS GENERATOR READS, AND NONE OF THEM IS THE EXPOSURE
//! VOCABULARY.
//!
//! `@tool`, `@shortdesc` and `@usewhen` are claimed, validated and refused by
//! the caller that writes `action.json`. They are not claimed here and they are
//! not refused here. A second copy of a vocabulary is a rule two programs get to
//! disagree about, and the disagreement is silent — both exit zero, and the
//! artifact carries whichever answer the last writer produced. So this program
//! hands over every comment in the file verbatim and states no opinion about
//! what any of them mean.
//!
//! WHAT IS LEFT HERE IS WHAT READING RUST REQUIRES, and the caller cannot do it
//! because it does not parse Rust.
//!
//! `@Payload` names the struct the schema is read from. Turning that name into
//! a declaration is resolution rather than vocabulary: the answer is a type in
//! this file, and only something holding the parsed file can find it. The name
//! itself still travels out in the comments, so the caller decides whether an
//! author may write it and this program decides only what it points at.
//!
//! The CONSTRAINT NAMES are listed for one purpose: reporting the gap. None of
//! them is claimed. TypeScript writes them in a member's doc comment and its
//! schema generator reads them; Go writes them in a struct tag; Rust has
//! neither and no vocabulary for them has been ruled on. So a Rust member that
//! writes one gets exactly what its author wrote — the line stays in the
//! description and no constraint reaches the schema — and the difference
//! between the languages, which is invisible from inside either one, is said
//! out loud instead of guessed at.

/// The struct an author may point at when their payload type is not named
/// `Payload`.
pub const PAYLOAD_ANNOTATION: &str = "Payload";

/// A source this generator will not describe.
///
/// Held apart from every other way the program can fail, because the caller
/// acts on the difference: a refused source has made the `action.json` already
/// on disk describe an action that no longer exists, and a missing toolchain
/// has not.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Refusal {
    pub message: String,
}

impl std::fmt::Display for Refusal {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(&self.message)
    }
}

pub fn refuse(action: &str, message: &str, accepted: &[String]) -> Refusal {
    if accepted.is_empty() {
        return Refusal {
            message: format!("{action}: {message}"),
        };
    }

    Refusal {
        message: format!("{action}: {message}. Accepted: {}", accepted.join(", ")),
    }
}

/// Every struct `@Payload` points at, in the order the file writes them.
///
/// A name is taken only when one follows the annotation on the same line. A
/// bare `@Payload` names nothing, and inventing a name for it would resolve the
/// schema off a word its author never wrote.
pub fn payload_structs_in(text: &str) -> Vec<String> {
    let mut named = Vec::new();

    for line in text.lines() {
        let trimmed = line.trim();

        if tag_name(trimmed).as_deref() != Some(PAYLOAD_ANNOTATION) {
            continue;
        }

        if let Some(struct_name) = trimmed.split_whitespace().nth(1) {
            named.push(struct_name.to_string());
        }
    }

    named
}

/// Whether a doc comment carries an annotation line of ANY vocabulary.
///
/// Used for one thing: picking which function describes an action when none of
/// them is called `handler`. An author who annotated a function has said which
/// function they were describing, whatever they called it and whichever
/// vocabulary they wrote in.
///
/// It asks about the SHAPE of a line rather than about a name, because naming
/// the exposure tags here would be this program claiming the vocabulary it has
/// just handed over. `@param`, `@see` and `@tool` all answer yes, and all three
/// mean the same thing for this question.
pub fn is_annotated(text: &str) -> bool {
    text.lines().any(|line| tag_name(line.trim()).is_some())
}

/// THE CONSTRAINT NAMES THE OTHER TWO GENERATORS TURN INTO SCHEMA KEYWORDS.
///
/// Listed only so the gap can be reported. Nothing here changes the artifact.
const CONSTRAINT_NAMES: [&str; 24] = [
    "minimum",
    "maximum",
    "exclusiveMinimum",
    "exclusiveMaximum",
    "multipleOf",
    "minLength",
    "maxLength",
    "pattern",
    "format",
    "minItems",
    "maxItems",
    "uniqueItems",
    "minProperties",
    "maxProperties",
    "enum",
    "default",
    "asType",
    "nullable",
    "const",
    "examples",
    "example",
    "additionalProperties",
    "readOnly",
    "writeOnly",
];

/// Every constraint name a comment writes, in the order it writes them.
pub fn constraint_names_in(text: &str) -> Vec<String> {
    let mut found = Vec::new();

    for line in text.lines() {
        let Some(name) = tag_name(line.trim()) else {
            continue;
        };

        if CONSTRAINT_NAMES.contains(&name.as_str()) && !found.contains(&name) {
            found.push(name);
        }
    }

    found
}

/// The annotation name a comment line begins with, if it begins with one at
/// all.
fn tag_name(line: &str) -> Option<String> {
    let rest = line.strip_prefix('@')?;
    let name = rest.split_whitespace().next().unwrap_or("");

    if name.is_empty() {
        return None;
    }

    Some(name.to_string())
}

#[cfg(test)]
mod tests {
    use super::{constraint_names_in, is_annotated, payload_structs_in};

    #[test]
    fn reads_the_struct_the_payload_annotation_points_at() {
        assert_eq!(
            payload_structs_in("Prose.\n@Payload InvoiceQuery\n"),
            vec!["InvoiceQuery".to_string()]
        );
    }

    #[test]
    fn a_bare_payload_annotation_names_nothing() {
        assert!(payload_structs_in("@Payload\n").is_empty());
    }

    #[test]
    fn every_payload_annotation_is_collected_so_two_can_be_told_apart() {
        assert_eq!(
            payload_structs_in("@Payload One\n@Payload Two\n"),
            vec!["One".to_string(), "Two".to_string()]
        );
    }

    #[test]
    fn the_exposure_vocabulary_is_not_read_here() {
        // The caller claims these. This module must not turn one into a
        // payload name, and must not treat a misspelling of one as anything at
        // all — a second opinion about the vocabulary is the thing this file
        // stopped holding.
        assert!(payload_structs_in("@tool\n@short_desc Totals invoices.\n").is_empty());
        assert!(payload_structs_in("@Payloud InvoiceQuery\n").is_empty());
    }

    #[test]
    fn an_annotation_of_any_vocabulary_marks_a_comment_as_annotated() {
        assert!(is_annotated("Prose.\n@tool\n"));
        assert!(is_annotated("@param name The name.\n"));
        assert!(is_annotated("  @Payload InvoiceQuery\n"));
        assert!(!is_annotated("Prose, and an address like a@b.\n"));
        assert!(!is_annotated(""));
    }

    #[test]
    fn names_every_constraint_a_comment_writes_once_each() {
        assert_eq!(
            constraint_names_in("How many.\n@minimum 1\n@maximum 50\n@minimum 2\n"),
            vec!["minimum".to_string(), "maximum".to_string()]
        );
        assert!(constraint_names_in("Just prose.\n").is_empty());
    }
}
