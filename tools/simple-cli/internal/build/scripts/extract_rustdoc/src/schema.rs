//! THE PAYLOAD'S SHAPE, READ OFF THE TYPE THAT DECLARES IT.
//!
//! A Rust member says what it accepts in its type and nowhere else. There is no
//! constraint vocabulary here: TypeScript writes `@minimum` and `@pattern` in a
//! member's doc comment and Go writes them in a struct tag, and Rust has
//! neither. NONE IS INVENTED. A doc comment that writes `@minimum 0` is prose,
//! it stays in the description exactly as its author wrote it, and no
//! `minimum` reaches the schema — because a constraint this generator made up
//! would be advertised to a model as a rule the action does not actually
//! enforce.
//!
//! A TYPE THIS MODULE CANNOT DESCRIBE IS REFUSED RATHER THAN APPROXIMATED.
//! The nearest thing to an approximation is an open object, and an open object
//! is what a model is told it may send anything into — so a member the
//! generator did not understand becomes a member the model fills in freely and
//! the action then rejects at runtime, with nothing in between saying so. A
//! refusal names the type and the member, and the author replaces it with one
//! that can be described.

use std::collections::{HashMap, HashSet};

use serde_json::{Map, Value};
use syn::{
    Fields, File, GenericArgument, Item, ItemEnum, ItemFn, ItemStruct, ItemType, PathArguments,
    Type,
};

use crate::serde_attrs::{container_attrs, member_attrs, ContainerAttrs, RenameRule};
use crate::tags::{constraint_names_in, refuse, Refusal};

/// Every type an action's own file declares, wherever it declares them —
/// including inside a `mod`, which a walk over the file's top-level items alone
/// would not see.
#[derive(Default)]
pub struct Declarations<'a> {
    pub structs: HashMap<String, &'a ItemStruct>,
    pub enums: HashMap<String, &'a ItemEnum>,
    pub aliases: HashMap<String, &'a ItemType>,
    pub functions: Vec<&'a ItemFn>,
    /// Names this file declares more than once, which the maps above cannot
    /// hold: a second declaration overwrites the first, so the type a name
    /// resolves to becomes whichever one the walk reached last.
    ambiguous: HashSet<String>,
}

impl Declarations<'_> {
    /// Whether a name is already taken by some declaration in this file.
    fn declares(&self, name: &str) -> bool {
        self.structs.contains_key(name)
            || self.enums.contains_key(name)
            || self.aliases.contains_key(name)
    }

    /// Whether a name resolves to more than one declaration.
    ///
    /// A NAME THAT DOES IS REFUSED WHEREVER IT IS RESOLVED, rather than
    /// answered with the last declaration read. Source order is not scope: a
    /// `Payload` inside a `mod` outranks the crate-root one the handler
    /// actually deserializes purely because it appears later, so moving a
    /// module up or down the file republishes a different contract with nothing
    /// said. Resolving it properly means resolving Rust paths, imports and
    /// visibility, which is a compiler; refusing says the true thing instead.
    pub fn is_ambiguous(&self, name: &str) -> bool {
        self.ambiguous.contains(name)
    }
}

pub fn collect_declarations(file: &File) -> Declarations<'_> {
    let mut declarations = Declarations::default();

    walk(&file.items, &mut declarations);

    declarations
}

fn walk<'a>(items: &'a [Item], into: &mut Declarations<'a>) {
    for item in items {
        match item {
            Item::Struct(declared) => {
                let name = claim(into, declared.ident.to_string());

                into.structs.insert(name, declared);
            }
            Item::Enum(declared) => {
                let name = claim(into, declared.ident.to_string());

                into.enums.insert(name, declared);
            }
            Item::Type(declared) => {
                let name = claim(into, declared.ident.to_string());

                into.aliases.insert(name, declared);
            }
            Item::Fn(declared) => into.functions.push(declared),
            Item::Mod(declared) => {
                if let Some((_, inner)) = &declared.content {
                    walk(inner, into);
                }
            }
            _ => {}
        }
    }
}

/// Records a declaration's name, noting it as ambiguous if the file has already
/// used it for something else.
fn claim(into: &mut Declarations<'_>, name: String) -> String {
    if into.declares(&name) {
        into.ambiguous.insert(name.clone());
    }

    name
}

/// The prose a declaration states about itself.
///
/// Rust carries a doc comment as a `#[doc = "..."]` attribute, one per line,
/// with the `/// ` marker already taken off and the single space after it left
/// on. That space is removed here so a line reads the same as the author typed
/// it, and nothing else is touched.
pub fn doc_text(attrs: &[syn::Attribute]) -> String {
    let mut lines = Vec::new();

    for attr in attrs {
        if !attr.path().is_ident("doc") {
            continue;
        }

        let syn::Meta::NameValue(named) = &attr.meta else {
            continue;
        };

        let syn::Expr::Lit(literal) = &named.value else {
            continue;
        };

        let syn::Lit::Str(text) = &literal.lit else {
            continue;
        };

        let value = text.value();

        for line in value.split('\n') {
            lines.push(line.strip_prefix(' ').unwrap_or(line).to_string());
        }
    }

    lines.join("\n")
}

/// The description a declaration states, EXACTLY AS ITS AUTHOR WROTE IT.
///
/// No line is lifted out. The caller owns the tag vocabulary and takes its own
/// lines out of this text once; taking them out here as well would be two
/// programs agreeing about a rule only one of them holds, and the day they stop
/// agreeing the description either keeps a `@tool` a model reads as prose or
/// loses a sentence its author wrote.
pub fn description_of(attrs: &[syn::Attribute]) -> String {
    doc_text(attrs).trim().to_string()
}

/// The canonical schema for an action that takes no input.
pub fn no_input_schema() -> Value {
    let mut schema = Map::new();

    schema.insert("additionalProperties".to_string(), Value::Bool(false));
    schema.insert("properties".to_string(), Value::Object(Map::new()));
    schema.insert("type".to_string(), Value::String("object".to_string()));

    Value::Object(schema)
}

pub struct SchemaBuilder<'a> {
    action: String,
    declarations: &'a Declarations<'a>,
    /// The types currently being described, so a type that reaches itself is
    /// refused rather than silently flattened into a bare object.
    visiting: Vec<String>,
    /// Members whose author asked for something this language has no way to
    /// state. Collected rather than refused, and reported rather than guessed
    /// at.
    gaps: Vec<String>,
    /// The member currently being described, so a gap found deep in its type
    /// names the member its author can see rather than the type it fell out of.
    member: String,
}

/// One member of a struct, resolved: what it is called on the wire, whether it
/// must be supplied, and the schema of what it accepts.
struct Member {
    name: String,
    required: bool,
    schema: Value,
}

impl<'a> SchemaBuilder<'a> {
    pub fn new(action: &str, declarations: &'a Declarations<'a>) -> Self {
        Self {
            action: action.to_string(),
            declarations,
            visiting: Vec::new(),
            gaps: Vec::new(),
            member: String::new(),
        }
    }

    /// What the schema could not say, in the words of the members that asked
    /// for it.
    pub fn gaps(&self) -> &[String] {
        &self.gaps
    }

    fn refuse(&self, message: &str) -> Refusal {
        refuse(&self.action, message, &[])
    }

    /// A name this file gives to more than one declaration.
    pub fn ambiguous(&self, name: &str) -> Refusal {
        self.refuse(&format!(
            "declares `{name}` more than once, so which declaration a member of that type \
             resolves to is decided by the order the declarations appear in. Moving a module up \
             or down the file would then republish a different contract"
        ))
    }

    /// The schema for the struct an action declares its payload as.
    pub fn payload_schema(&mut self, item: &ItemStruct) -> Result<Value, Refusal> {
        let Fields::Named(_) = &item.fields else {
            return Err(self.refuse(&format!(
                "declares its payload as `{}`, which has no named fields. A payload is a JSON \
                 object, so its type must be a struct with named fields",
                item.ident
            )));
        };

        self.named_struct_schema(item)
    }

    fn named_struct_schema(&mut self, item: &ItemStruct) -> Result<Value, Refusal> {
        let name = item.ident.to_string();

        if self.visiting.contains(&name) {
            return Err(self.refuse(&format!(
                "declares `{name}`, which contains itself. A recursive payload has no finite \
                 schema, so an agent cannot be told what it accepts"
            )));
        }

        self.visiting.push(name);
        let members = self.members_of(item);
        self.visiting.pop();

        let members = members?;

        let mut properties = Map::new();
        let mut required = Vec::new();

        for member in members {
            // TWO MEMBERS ON ONE WIRE NAME AND ONE OF THEM DISAPPEARS.
            //
            // A map keeps whichever arrived last and says nothing about the
            // other, so the schema describes an action that takes one member
            // where it takes two — and every call a model makes is missing a
            // field the action requires, for a reason nothing in the artifact
            // records. `serde` will not accept the struct either, so the source
            // is wrong rather than merely undescribable, and saying so here is
            // the earliest an author hears it.
            if properties.contains_key(&member.name) {
                return Err(self.refuse(&format!(
                    "declares two members called `{}` on the wire. A JSON object names each \
                     member once, so one of them could only be described by deleting the other",
                    member.name
                )));
            }

            if member.required {
                required.push(member.name.clone());
            }

            properties.insert(member.name, member.schema);
        }

        let mut schema = Map::new();

        // `properties` is stated even when it is empty, so a payload with no
        // members reads as an object that accepts none rather than as an object
        // nobody described.
        schema.insert("properties".to_string(), Value::Object(properties));

        if !required.is_empty() {
            schema.insert(
                "required".to_string(),
                Value::Array(required.into_iter().map(Value::String).collect()),
            );
        }

        schema.insert("type".to_string(), Value::String("object".to_string()));

        Ok(Value::Object(schema))
    }

    fn members_of(&mut self, item: &ItemStruct) -> Result<Vec<Member>, Refusal> {
        let container: ContainerAttrs = container_attrs(&self.action, &item.attrs)?;
        let mut members = Vec::new();

        for field in &item.fields {
            let Some(ident) = &field.ident else {
                continue;
            };

            let attrs = member_attrs(&self.action, &field.attrs)?;

            if attrs.skip {
                continue;
            }

            if attrs.flatten {
                members.extend(self.flattened_members(&field.ty, ident.to_string())?);
                continue;
            }

            let name = wire_name(&ident.to_string(), &attrs.rename, container.rename_all);
            let optional = strip_option(&field.ty).is_some() || attrs.default || container.default;
            let inner = strip_option(&field.ty).unwrap_or(&field.ty);

            self.member = name.clone();

            let mut schema = self.type_schema(inner)?;
            let documented = doc_text(&field.attrs);
            // Exactly what the author wrote, for the reason `description_of`
            // gives: the caller owns the tag vocabulary and lifts its own lines
            // out once.
            let description = documented.trim().to_string();

            // A CONSTRAINT THIS LANGUAGE CANNOT STATE IS NAMED RATHER THAN
            // INVENTED OR IGNORED.
            //
            // The author wrote a rule and the schema will not carry it, which
            // is a difference nobody can see from inside the source: the line
            // is still there in the description, and the build is green. So it
            // is said out loud, once per member, and the artifact is left
            // exactly as the type describes it.
            for named in constraint_names_in(&documented) {
                self.gaps.push(format!(
                    "`{name}` writes @{named}, and a Rust member has no way to state a \
                     constraint. The line stays in the description and no `{named}` reaches the \
                     schema",
                ));
            }

            if !description.is_empty() {
                if let Value::Object(members) = &mut schema {
                    members.insert("description".to_string(), Value::String(description));
                }
            }

            members.push(Member {
                name,
                required: !optional,
                schema,
            });
        }

        Ok(members)
    }

    /// `#[serde(flatten)]` puts another struct's members in this one's place, so
    /// the schema does the same. Left as an ordinary property it would advertise
    /// a nesting level the deserializer will not accept.
    fn flattened_members(&mut self, ty: &Type, field: String) -> Result<Vec<Member>, Refusal> {
        let inner = strip_option(ty).unwrap_or(ty);
        let optional = strip_option(ty).is_some();

        if let Some(named) = last_segment_name(unwrap_transparent(inner)) {
            if self.declarations.is_ambiguous(&named) {
                return Err(self.ambiguous(&named));
            }
        }

        let Some(item) = self.declared_struct(inner) else {
            return Err(self.refuse(&format!(
                "flattens `{field}`, whose type is not a struct declared in this file. A flattened \
                 member's own members are what the action accepts, and they cannot be read from here"
            )));
        };

        let name = item.ident.to_string();

        if self.visiting.contains(&name) {
            return Err(self.refuse(&format!(
                "flattens `{name}` into itself, which has no finite schema"
            )));
        }

        self.visiting.push(name);
        let members = self.members_of(item);
        self.visiting.pop();

        let mut members = members?;

        if optional {
            for member in &mut members {
                member.required = false;
            }
        }

        Ok(members)
    }

    fn declared_struct(&self, ty: &Type) -> Option<&'a ItemStruct> {
        let name = last_segment_name(unwrap_transparent(ty))?;

        self.declarations.structs.get(&name).copied()
    }

    pub fn type_schema(&mut self, ty: &Type) -> Result<Value, Refusal> {
        let ty = unwrap_transparent(ty);

        match ty {
            Type::Path(_) => self.path_schema(ty),
            Type::Array(array) => {
                let items = self.type_schema(&array.elem)?;

                Ok(array_schema(items))
            }
            Type::Slice(slice) => {
                let items = self.type_schema(&slice.elem)?;

                Ok(array_schema(items))
            }
            other => Err(self.refuse(&format!(
                "has a member typed `{}`, which this generator cannot describe as JSON",
                rendered(other)
            ))),
        }
    }

    fn path_schema(&mut self, ty: &Type) -> Result<Value, Refusal> {
        let Some(name) = last_segment_name(ty) else {
            return Err(self.refuse(&format!(
                "has a member typed `{}`, which names no type",
                rendered(ty)
            )));
        };

        let arguments = type_arguments(ty);

        match name.as_str() {
            "String" | "str" | "char" => return Ok(scalar("string")),
            "i8" | "i16" | "i32" | "i64" | "i128" | "isize" | "u8" | "u16" | "u32" | "u64"
            | "u128" | "usize" => return Ok(scalar("integer")),
            "f32" | "f64" => return Ok(scalar("number")),
            "bool" => return Ok(scalar("boolean")),
            // AN `Option` REACHED HERE IS NESTED INSIDE A CONTAINER, WHERE
            // "OPTIONAL" HAS NOWHERE TO BE STATED.
            //
            // A member's own `Option` is stripped before this point and turns
            // into absence from `required`. An element's cannot: JSON Schema
            // says an array element may be null with a type union, and no
            // vocabulary for one has been ruled on here. So the element is
            // described as the type the `Option` contains — `Vec<Option<T>>`
            // becomes an array of `T` — and the narrowing is SAID rather than
            // performed in silence. The action accepts a null the schema does
            // not mention, which nobody can see from inside either the source
            // or the artifact.
            "Option" => {
                let Some(inner) = arguments.first() else {
                    return Err(self.refuse(&format!(
                        "has a member typed `{}`, which names no contained type",
                        rendered(ty)
                    )));
                };

                let member = self.member.clone();

                self.gaps.push(format!(
                    "`{member}` writes `Option` inside a container, and an element has no way to \
                     state that it may be null. The element is described as the type the `Option` \
                     contains, so the schema does not mention a null the action accepts",
                ));

                return self.type_schema(inner);
            }
            "Box" | "Rc" | "Arc" | "Cow" => {
                let Some(inner) = arguments.first() else {
                    return Err(self.refuse(&format!(
                        "has a member typed `{}`, which names no contained type",
                        rendered(ty)
                    )));
                };

                return self.type_schema(inner);
            }
            "Vec" | "VecDeque" | "HashSet" | "BTreeSet" => {
                let Some(inner) = arguments.first() else {
                    return Err(self.refuse(&format!(
                        "has a member typed `{}`, which names no element type",
                        rendered(ty)
                    )));
                };

                let items = self.type_schema(inner)?;

                return Ok(array_schema(items));
            }
            "HashMap" | "BTreeMap" | "Map" => return self.map_schema(ty, &arguments),
            _ => {}
        }

        if self.declarations.is_ambiguous(&name) {
            return Err(self.ambiguous(&name));
        }

        if let Some(alias) = self.declarations.aliases.get(&name).copied() {
            return self.alias_schema(&name, alias);
        }

        if let Some(declared) = self.declarations.structs.get(&name).copied() {
            return self.declared_struct_schema(declared);
        }

        if let Some(declared) = self.declarations.enums.get(&name).copied() {
            return self.enum_schema(declared);
        }

        // `serde_json::Value` is any JSON value at all, which is the one type
        // whose honest schema states nothing. It is checked after the file's own
        // declarations so an action that declares a type of its own called
        // `Value` is described by that type.
        if name == "Value" {
            return Ok(Value::Object(Map::new()));
        }

        Err(self.refuse(&format!(
            "has a member typed `{name}`, which is declared nowhere in this file and is not a \
             type this generator knows. Describe the member with a type the schema can state"
        )))
    }

    fn alias_schema(&mut self, name: &str, alias: &ItemType) -> Result<Value, Refusal> {
        if self.visiting.iter().any(|visited| visited == name) {
            return Err(self.refuse(&format!("declares `{name}`, which is defined as itself")));
        }

        self.visiting.push(name.to_string());
        let resolved = self.type_schema(&alias.ty);
        self.visiting.pop();

        resolved
    }

    fn declared_struct_schema(&mut self, item: &ItemStruct) -> Result<Value, Refusal> {
        match &item.fields {
            Fields::Named(_) => self.named_struct_schema(item),
            // A newtype is what `serde` reads straight through, so the schema
            // reads straight through it too.
            Fields::Unnamed(fields) if fields.unnamed.len() == 1 => {
                let name = item.ident.to_string();

                if self.visiting.contains(&name) {
                    return Err(self.refuse(&format!("declares `{name}`, which contains itself")));
                }

                self.visiting.push(name);
                let resolved = self.type_schema(&fields.unnamed[0].ty);
                self.visiting.pop();

                resolved
            }
            _ => Err(self.refuse(&format!(
                "has a member typed `{}`, a struct with no named fields. A tuple or unit struct \
                 has no shape an agent can be told to fill in",
                item.ident
            ))),
        }
    }

    fn enum_schema(&mut self, item: &ItemEnum) -> Result<Value, Refusal> {
        let container = container_attrs(&self.action, &item.attrs)?;
        let mut values = Vec::new();

        for variant in &item.variants {
            if !matches!(variant.fields, Fields::Unit) {
                return Err(self.refuse(&format!(
                    "has a member typed `{}`, an enum whose variant `{}` carries data. How such an \
                     enum is represented in JSON depends on a serde tagging attribute this \
                     generator does not interpret",
                    item.ident, variant.ident
                )));
            }

            let attrs = member_attrs(&self.action, &variant.attrs)?;
            let name = variant.ident.to_string();

            values.push(Value::String(match (&attrs.rename, container.rename_all) {
                (Some(renamed), _) => renamed.clone(),
                (None, Some(rule)) => rule.apply_to_variant(&name),
                (None, None) => name,
            }));
        }

        if values.is_empty() {
            return Err(self.refuse(&format!(
                "has a member typed `{}`, an enum with no variants, so no value can ever satisfy it",
                item.ident
            )));
        }

        let mut schema = Map::new();

        schema.insert("enum".to_string(), Value::Array(values));
        schema.insert("type".to_string(), Value::String("string".to_string()));

        Ok(Value::Object(schema))
    }

    fn map_schema(&mut self, ty: &Type, arguments: &[&Type]) -> Result<Value, Refusal> {
        let (key, value) = match arguments {
            // `serde_json::Map` states only its value type in practice, but both
            // forms are written.
            [value] => (None, *value),
            [key, value] => (Some(*key), *value),
            _ => {
                return Err(self.refuse(&format!(
                    "has a member typed `{}`, whose key and value types cannot be read",
                    rendered(ty)
                )))
            }
        };

        if let Some(key) = key {
            let named = last_segment_name(unwrap_transparent(key));

            if !matches!(named.as_deref(), Some("String") | Some("str")) {
                return Err(self.refuse(&format!(
                    "has a member typed `{}`, whose key is not a string. A JSON object names its \
                     members with strings and nothing else",
                    rendered(ty)
                )));
            }
        }

        let value_schema = self.type_schema(value)?;
        let mut schema = Map::new();

        // A value type that states nothing is an object that accepts anything,
        // which JSON Schema says with `true` rather than with an empty schema.
        schema.insert(
            "additionalProperties".to_string(),
            match &value_schema {
                Value::Object(members) if members.is_empty() => Value::Bool(true),
                _ => value_schema,
            },
        );

        schema.insert("type".to_string(), Value::String("object".to_string()));

        Ok(Value::Object(schema))
    }
}

fn wire_name(field: &str, rename: &Option<String>, rename_all: Option<RenameRule>) -> String {
    match (rename, rename_all) {
        (Some(renamed), _) => renamed.clone(),
        (None, Some(rule)) => rule.apply_to_field(field),
        (None, None) => field.to_string(),
    }
}

fn scalar(name: &str) -> Value {
    let mut schema = Map::new();

    schema.insert("type".to_string(), Value::String(name.to_string()));

    Value::Object(schema)
}

fn array_schema(items: Value) -> Value {
    let mut schema = Map::new();

    schema.insert("items".to_string(), items);
    schema.insert("type".to_string(), Value::String("array".to_string()));

    Value::Object(schema)
}

/// The type an `Option` wraps, if the type is one.
///
/// OPTIONALITY IS THE TYPE and nothing else. There is no tag for an author to
/// write, and the mapping is of what `Option` CONTAINS — marked optional rather
/// than nullable, because a member that may be omitted is not the same claim as
/// a member that may be sent as `null`.
pub fn strip_option(ty: &Type) -> Option<&Type> {
    let ty = unwrap_transparent(ty);

    if last_segment_name(ty).as_deref() != Some("Option") {
        return None;
    }

    let arguments = type_arguments(ty);
    let inner = *arguments.first()?;

    Some(strip_option(inner).unwrap_or(inner))
}

/// The type behind the syntax that does not change it: a reference, a
/// parenthesised type, or the invisible group a macro leaves behind.
fn unwrap_transparent(ty: &Type) -> &Type {
    match ty {
        Type::Reference(reference) => unwrap_transparent(&reference.elem),
        Type::Paren(paren) => unwrap_transparent(&paren.elem),
        Type::Group(group) => unwrap_transparent(&group.elem),
        other => other,
    }
}

fn last_segment_name(ty: &Type) -> Option<String> {
    match ty {
        Type::Path(path) => path.path.segments.last().map(|last| last.ident.to_string()),
        _ => None,
    }
}

fn type_arguments(ty: &Type) -> Vec<&Type> {
    let Type::Path(path) = ty else {
        return Vec::new();
    };

    let Some(last) = path.path.segments.last() else {
        return Vec::new();
    };

    let PathArguments::AngleBracketed(arguments) = &last.arguments else {
        return Vec::new();
    };

    arguments
        .args
        .iter()
        .filter_map(|argument| match argument {
            GenericArgument::Type(ty) => Some(ty),
            _ => None,
        })
        .collect()
}

/// A type named the way its author would recognise it, so a refusal says what
/// it refused rather than which branch of the parser it fell out of.
fn rendered(ty: &Type) -> String {
    match ty {
        Type::Path(path) => {
            let segments: Vec<String> = path
                .path
                .segments
                .iter()
                .map(|segment| segment.ident.to_string())
                .collect();

            segments.join("::")
        }
        Type::Tuple(tuple) if tuple.elems.is_empty() => "()".to_string(),
        Type::Tuple(_) => "a tuple".to_string(),
        Type::TraitObject(_) => "a trait object".to_string(),
        Type::ImplTrait(_) => "an impl Trait".to_string(),
        Type::Ptr(_) => "a raw pointer".to_string(),
        Type::BareFn(_) => "a function pointer".to_string(),
        Type::Never(_) => "!".to_string(),
        Type::Infer(_) => "_".to_string(),
        Type::Macro(_) => "a macro invocation".to_string(),
        _ => "a type with no JSON shape".to_string(),
    }
}
