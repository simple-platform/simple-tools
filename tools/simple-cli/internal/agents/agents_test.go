package agents

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"simple-cli/internal/agents/structure"
)

const capturedParserFixtureGZIP = `H4sICNDdB2sAA2NvbXBsZXRlLWFwcC5qc29uAM1ZS0/rOBTez6+IIqSCREvSNH0tRrqb0SxGGunOEpBlEgO5dZMQJ2Uqhv8+dh4nLdg+4VKkywISPsc+x993HnGuX9yUbpm7dnNOy/us2JIyyzjJiyxiQpCYltS9dMt9rsbc8SzayNsN28u7HS3kdfSY8Lhgqbu+funGbXYS2FFeqZub1HGeKlbsnQdWEvMyzosa6Tg8e0iitROzHRHJNueMiL0o2ZaQGhHnzTDHeX5kBVt3j6kfmuc8iWiZZClJYgkR9rR2blw516SZa9LMdeO+XvaPqR3oB7c2jZVNclw3rL24cF6cJG7u6l+wHbWPzZ20NHLXLy5PUrkDvtykjLvr4LXdFD28en291TzttbAnn1aG6tHAf329BC6z55TQqN4GnmWbKj8phf3sX0hZlG0nNNpKwh6z3EZX9MiizTgrYlachq0ZQtfMzpcftLivJQzgaXDA2LX7bu8mmzR75ix+YNLKnBUiSykRVZ5nReneGvgUrBzC5z/NNM63B5aW8HCZlJzptmS6sG8J4KvaJd2KZ/cJZ+cj8Omq9WTcejbZxqMLsOQui7XcTJeIIR2+1HMznVu5ATj0jrg5YoLT/Z2MKVKwsipS8VkqvjfTON28OBvB1L4JgGNs3PSOXbXejDszJB83Lk5IECC2BHZCAt9KCMDBR4JFbBLOJT/3VRqTPJPpZf95ltRkTjsZThESMMHPBIzyatx4NW4MGRY0ARI0ARI0gT1oAA5nH+Corv73iSSoztyf5ecPOZPzdz0TSs4MiZ/Z0Pg5IEe5M1buNIVoGDEzJHhmSPDM7MED8Jts9p4Y2paB+u/X1pg4ESrFkNoYnU9I5ADuz43sfEvFs6wqqmMRqq8Qk355JqIiydV/tasjoQK4H5q1ITsamXwiKQSZYd8U7sOcmqSiLKq6jxKy9dHas0Ls6XDfrFXVqe1Yz15Jy0roFgs9+2KA+55Bj/ZEAfAMSxQlFRtS+yIl2F2fquB203SPmZQYIn0g4Evj1n9/s1TOq4Jyo/bDEFkxxNX3J01jzoTTejlY+eEcWXv+YeW/aZQOpQ+YXvYhkgQAtxnTpOjGHHGlZDRW40TX6kx+yMAcXTj//e6cqcvz3jwh3ym2VGsZkiAA9z20+dKYpoKTNZbJ7dKaVo/QmobkCsDNpgVzz+sWeqYbRpK0ZIUEiZC2prF23TmSNgCfhvq0EdpfmAAOp0PKGLEmDxKze1rxkjQl75Op5FDwo6PieVD9m//rhT5Hcgzg/nRg1L3z+DDsetBgDpKAALcUX/Yvi6oy6/uwItO3YXMk4wBuzK+yejIopkCtdi0koQDuz/Qands1CvAC02hPQd3PazUa0aJImCBt73BKlX5SIAvkKAtwi0AG90fQXZuMQfp4wM3G+JDu8kwkptq4QPp0wFWU6tSz8KzqAXgxuDEaop7uQOEXkg8ShosBzf3wJgPVD1LLATdbMx2mH6QyA27Uj72xBngRnlA/X3CKckIpLZGYBHywlDT+fkRNS6SUA242KBikpiVSpAE3qWk5taoJ4OV8sJrU8YdWTFv5qh9RfsqjnlOKCAlMwAeLqHfz0I76I4jBhhXSPgNupHNpp7ODF4sT0nn0efDXIXSFBOEKD0L3bEeL85H5M2X3yvh0PprUxF57t5Pk8LzPSjcSvSsself2ThTgxQqhu7ES6sDxeSzh0l2Zf05K7U8EBxKgq8PKOez96NjPj2R430NCtR+g5KUlzx6rAIeDyJNGni6t9ofX6MmY72GfSD38bExzYo+dDfseEjz9AEt8/yXbReFUuVM7O/yEzPeQF9Z+gOVYavBprO8hzXI/wHQe63vIp2jAZ76c4bf/ATWVJ0oMIgAA`

// Captured from the SCL parser for the inline shape cases below.
const capturedInlineShapeRulesSCL = `set dev_simple_system.task_type, task_type_returns {
  name "returns"
  schema {
    input {
      type "object"
      properties {
        order_id {
          type "string"
        }
      }
    }
    output {
      type "object"
    }
  }
}

set dev_simple_system.task_type, task_type_input_true {
  name "input_true"
  schema {
    input true
    output {
      type "object"
    }
  }
}

set dev_simple_system.task_type, task_type_input_empty {
  name "input_empty"
  schema {
    input {}
    output {
      type "object"
    }
  }
}

set dev_simple_system.task_type, task_type_output_null {
  name "output_null"
  schema {
    input true
    output null
  }
}

set dev_simple_system.task_type, task_type_output_quoted_null {
  name "output_quoted_null"
  schema {
    input true
    output "null"
  }
}

set dev_simple_system.task_type, task_type_input_null {
  name "input_null"
  schema {
    input null
    output {
      type "object"
    }
  }
}

set dev_simple_system.task_type, task_type_input_absent {
  name "input_absent"
  schema {
    output {
      type "object"
    }
  }
}

set dev_simple_system.task_type, task_type_input_bad_type {
  name "input_bad_type"
  schema {
    input {
      type "string"
    }
    output {
      type "object"
    }
  }
}

set dev_simple_system.task_type, task_type_output_bad_type {
  name "output_bad_type"
  schema {
    input true
    output {
      type "string"
    }
  }
}

set dev_simple_system.task_type, task_type_inline_object {
  name "inline_object"
  schema {
    input {
      type "object"
      order_id {
        type "string"
      }
    }
    output {
      type "object"
      accepted {
        type "boolean"
      }
    }
  }
}

set dev_simple_system.task_type, task_type_output_absent {
  name "output_absent"
  schema {
    input true
  }
}
`

const capturedInlineShapeRulesParserOutput = `[{"name":["dev_simple_system.task_type","task_type_returns"],"type":"block","key":"set","children":[{"type":"kv","value":"returns","key":"name","key_loc":{"line":2,"col":3},"value_loc":{"line":2,"col":8}},{"name":"","type":"block","key":"schema","children":[{"name":"","type":"block","key":"input","children":[{"type":"kv","value":"object","key":"type","key_loc":{"line":5,"col":7},"value_loc":{"line":5,"col":12}},{"name":"","type":"block","key":"properties","children":[{"name":"","type":"block","key":"order_id","children":[{"type":"kv","value":"string","key":"type","key_loc":{"line":8,"col":11},"value_loc":{"line":8,"col":16}}],"key_loc":{"line":7,"col":9},"name_loc":{"line":7,"col":18}}],"key_loc":{"line":6,"col":7},"name_loc":{"line":6,"col":18}}],"key_loc":{"line":4,"col":5},"name_loc":{"line":4,"col":11}},{"name":"","type":"block","key":"output","children":[{"type":"kv","value":"object","key":"type","key_loc":{"line":13,"col":7},"value_loc":{"line":13,"col":12}}],"key_loc":{"line":12,"col":5},"name_loc":{"line":12,"col":12}}],"key_loc":{"line":3,"col":3},"name_loc":{"line":3,"col":10}}],"key_loc":{"line":1,"col":1},"name_loc":{"line":1,"col":52}},{"name":["dev_simple_system.task_type","task_type_input_true"],"type":"block","key":"set","children":[{"type":"kv","value":"input_true","key":"name","key_loc":{"line":19,"col":3},"value_loc":{"line":19,"col":8}},{"name":"","type":"block","key":"schema","children":[{"type":"kv","value":true,"key":"input","key_loc":{"line":21,"col":5},"value_loc":{"line":21,"col":11}},{"name":"","type":"block","key":"output","children":[{"type":"kv","value":"object","key":"type","key_loc":{"line":23,"col":7},"value_loc":{"line":23,"col":12}}],"key_loc":{"line":22,"col":5},"name_loc":{"line":22,"col":12}}],"key_loc":{"line":20,"col":3},"name_loc":{"line":20,"col":10}}],"key_loc":{"line":18,"col":1},"name_loc":{"line":18,"col":55}},{"name":["dev_simple_system.task_type","task_type_input_empty"],"type":"block","key":"set","children":[{"type":"kv","value":"input_empty","key":"name","key_loc":{"line":29,"col":3},"value_loc":{"line":29,"col":8}},{"name":"","type":"block","key":"schema","children":[{"name":"","type":"block","key":"input","children":[],"key_loc":{"line":31,"col":5},"name_loc":{"line":31,"col":11}},{"name":"","type":"block","key":"output","children":[{"type":"kv","value":"object","key":"type","key_loc":{"line":33,"col":7},"value_loc":{"line":33,"col":12}}],"key_loc":{"line":32,"col":5},"name_loc":{"line":32,"col":12}}],"key_loc":{"line":30,"col":3},"name_loc":{"line":30,"col":10}}],"key_loc":{"line":28,"col":1},"name_loc":{"line":28,"col":56}},{"name":["dev_simple_system.task_type","task_type_output_null"],"type":"block","key":"set","children":[{"type":"kv","value":"output_null","key":"name","key_loc":{"line":39,"col":3},"value_loc":{"line":39,"col":8}},{"name":"","type":"block","key":"schema","children":[{"type":"kv","value":true,"key":"input","key_loc":{"line":41,"col":5},"value_loc":{"line":41,"col":11}},{"type":"kv","value":"null","key":"output","key_loc":{"line":42,"col":5},"value_loc":{"line":42,"col":12}}],"key_loc":{"line":40,"col":3},"name_loc":{"line":40,"col":10}}],"key_loc":{"line":38,"col":1},"name_loc":{"line":38,"col":56}},{"name":["dev_simple_system.task_type","task_type_output_quoted_null"],"type":"block","key":"set","children":[{"type":"kv","value":"output_quoted_null","key":"name","key_loc":{"line":47,"col":3},"value_loc":{"line":47,"col":8}},{"name":"","type":"block","key":"schema","children":[{"type":"kv","value":true,"key":"input","key_loc":{"line":49,"col":5},"value_loc":{"line":49,"col":11}},{"type":"kv","value":"null","key":"output","key_loc":{"line":50,"col":5},"value_loc":{"line":50,"col":12}}],"key_loc":{"line":48,"col":3},"name_loc":{"line":48,"col":10}}],"key_loc":{"line":46,"col":1},"name_loc":{"line":46,"col":63}},{"name":["dev_simple_system.task_type","task_type_input_null"],"type":"block","key":"set","children":[{"type":"kv","value":"input_null","key":"name","key_loc":{"line":55,"col":3},"value_loc":{"line":55,"col":8}},{"name":"","type":"block","key":"schema","children":[{"type":"kv","value":"null","key":"input","key_loc":{"line":57,"col":5},"value_loc":{"line":57,"col":11}},{"name":"","type":"block","key":"output","children":[{"type":"kv","value":"object","key":"type","key_loc":{"line":59,"col":7},"value_loc":{"line":59,"col":12}}],"key_loc":{"line":58,"col":5},"name_loc":{"line":58,"col":12}}],"key_loc":{"line":56,"col":3},"name_loc":{"line":56,"col":10}}],"key_loc":{"line":54,"col":1},"name_loc":{"line":54,"col":55}},{"name":["dev_simple_system.task_type","task_type_input_absent"],"type":"block","key":"set","children":[{"type":"kv","value":"input_absent","key":"name","key_loc":{"line":65,"col":3},"value_loc":{"line":65,"col":8}},{"name":"","type":"block","key":"schema","children":[{"name":"","type":"block","key":"output","children":[{"type":"kv","value":"object","key":"type","key_loc":{"line":68,"col":7},"value_loc":{"line":68,"col":12}}],"key_loc":{"line":67,"col":5},"name_loc":{"line":67,"col":12}}],"key_loc":{"line":66,"col":3},"name_loc":{"line":66,"col":10}}],"key_loc":{"line":64,"col":1},"name_loc":{"line":64,"col":57}},{"name":["dev_simple_system.task_type","task_type_input_bad_type"],"type":"block","key":"set","children":[{"type":"kv","value":"input_bad_type","key":"name","key_loc":{"line":74,"col":3},"value_loc":{"line":74,"col":8}},{"name":"","type":"block","key":"schema","children":[{"name":"","type":"block","key":"input","children":[{"type":"kv","value":"string","key":"type","key_loc":{"line":77,"col":7},"value_loc":{"line":77,"col":12}}],"key_loc":{"line":76,"col":5},"name_loc":{"line":76,"col":11}},{"name":"","type":"block","key":"output","children":[{"type":"kv","value":"object","key":"type","key_loc":{"line":80,"col":7},"value_loc":{"line":80,"col":12}}],"key_loc":{"line":79,"col":5},"name_loc":{"line":79,"col":12}}],"key_loc":{"line":75,"col":3},"name_loc":{"line":75,"col":10}}],"key_loc":{"line":73,"col":1},"name_loc":{"line":73,"col":59}},{"name":["dev_simple_system.task_type","task_type_output_bad_type"],"type":"block","key":"set","children":[{"type":"kv","value":"output_bad_type","key":"name","key_loc":{"line":86,"col":3},"value_loc":{"line":86,"col":8}},{"name":"","type":"block","key":"schema","children":[{"type":"kv","value":true,"key":"input","key_loc":{"line":88,"col":5},"value_loc":{"line":88,"col":11}},{"name":"","type":"block","key":"output","children":[{"type":"kv","value":"string","key":"type","key_loc":{"line":90,"col":7},"value_loc":{"line":90,"col":12}}],"key_loc":{"line":89,"col":5},"name_loc":{"line":89,"col":12}}],"key_loc":{"line":87,"col":3},"name_loc":{"line":87,"col":10}}],"key_loc":{"line":85,"col":1},"name_loc":{"line":85,"col":60}},{"name":["dev_simple_system.task_type","task_type_inline_object"],"type":"block","key":"set","children":[{"type":"kv","value":"inline_object","key":"name","key_loc":{"line":96,"col":3},"value_loc":{"line":96,"col":8}},{"name":"","type":"block","key":"schema","children":[{"name":"","type":"block","key":"input","children":[{"type":"kv","value":"object","key":"type","key_loc":{"line":99,"col":7},"value_loc":{"line":99,"col":12}},{"name":"","type":"block","key":"order_id","children":[{"type":"kv","value":"string","key":"type","key_loc":{"line":101,"col":9},"value_loc":{"line":101,"col":14}}],"key_loc":{"line":100,"col":7},"name_loc":{"line":100,"col":16}}],"key_loc":{"line":98,"col":5},"name_loc":{"line":98,"col":11}},{"name":"","type":"block","key":"output","children":[{"type":"kv","value":"object","key":"type","key_loc":{"line":105,"col":7},"value_loc":{"line":105,"col":12}},{"name":"","type":"block","key":"accepted","children":[{"type":"kv","value":"boolean","key":"type","key_loc":{"line":107,"col":9},"value_loc":{"line":107,"col":14}}],"key_loc":{"line":106,"col":7},"name_loc":{"line":106,"col":16}}],"key_loc":{"line":104,"col":5},"name_loc":{"line":104,"col":12}}],"key_loc":{"line":97,"col":3},"name_loc":{"line":97,"col":10}}],"key_loc":{"line":95,"col":1},"name_loc":{"line":95,"col":58}},{"name":["dev_simple_system.task_type","task_type_output_absent"],"type":"block","key":"set","children":[{"type":"kv","value":"output_absent","key":"name","key_loc":{"line":114,"col":3},"value_loc":{"line":114,"col":8}},{"name":"","type":"block","key":"schema","children":[{"type":"kv","value":true,"key":"input","key_loc":{"line":116,"col":5},"value_loc":{"line":116,"col":11}}],"key_loc":{"line":115,"col":3},"name_loc":{"line":115,"col":10}}],"key_loc":{"line":113,"col":1},"name_loc":{"line":113,"col":58}}]`

const capturedAgentTaskTypeLinkParserOutput = `[{"name":["agent_task_type","link_people"],"type":"block","key":"set","children":[],"key_loc":{"line":1,"col":1},"name_loc":{"line":1,"col":34}}]`

const capturedNestedBlockListParserOutput = `[{"name":["dev_simple_system.task_type","task_type_returns"],"type":"block","key":"set","children":[{"type":"kv","value":"returns","key":"name","key_loc":{"line":2,"col":3},"value_loc":{"line":2,"col":8}},{"name":"","type":"block","key":"schema","children":[{"name":"","type":"block","key":"input","children":[{"name":"","type":"block","key":"order_id","children":[{"type":"kv","value":"string","key":"type","key_loc":{"line":6,"col":9},"value_loc":{"line":6,"col":14}}],"key_loc":{"line":5,"col":7},"name_loc":{"line":5,"col":9}},{"type":"kv_multi","value":[{"line":8,"value":"a","col":16},{"line":8,"value":"b","col":21}],"key":"required","key_loc":{"line":8,"col":7}}],"key_loc":{"line":4,"col":5},"name_loc":{"line":4,"col":11}},{"type":"kv","value":"null","key":"output","key_loc":{"line":10,"col":5},"value_loc":{"line":10,"col":12}}],"key_loc":{"line":3,"col":3},"name_loc":{"line":3,"col":10}}],"key_loc":{"line":1,"col":1},"name_loc":{"line":1,"col":52}}]`

const capturedNestedBlockThreeListParserOutput = `[{"name":["dev_simple_system.task_type","task_type_returns"],"type":"block","key":"set","children":[{"type":"kv","value":"returns","key":"name","key_loc":{"line":2,"col":3},"value_loc":{"line":2,"col":8}},{"name":"","type":"block","key":"schema","children":[{"name":"","type":"block","key":"input","children":[{"name":"","type":"block","key":"order_id","children":[{"type":"kv","value":"string","key":"type","key_loc":{"line":6,"col":9},"value_loc":{"line":6,"col":14}}],"key_loc":{"line":5,"col":7},"name_loc":{"line":5,"col":9}},{"type":"kv_multi","value":[{"line":8,"value":"a","col":16},{"line":8,"value":"b","col":21},{"line":8,"value":"c","col":26}],"key":"required","key_loc":{"line":8,"col":7}}],"key_loc":{"line":4,"col":5},"name_loc":{"line":4,"col":11}},{"type":"kv","value":"null","key":"output","key_loc":{"line":10,"col":5},"value_loc":{"line":10,"col":12}}],"key_loc":{"line":3,"col":3},"name_loc":{"line":3,"col":10}}],"key_loc":{"line":1,"col":1},"name_loc":{"line":1,"col":52}}]`

const fixtureAppID = "com.acme.shop"
const fixtureRepoRoot = "/repo"

func parserFixture(t *testing.T) []byte {
	t.Helper()
	encoded, err := base64.StdEncoding.DecodeString(capturedParserFixtureGZIP)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Errorf("close parser fixture: %v", err)
		}
	}()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.ReplaceAll(data, []byte("knowledge/returns-playbook.md"), []byte("knowledge/playbook-returns.md"))
}

func fixtureSource(t *testing.T) []byte {
	t.Helper()
	var blocks []struct {
		Key  string `json:"key"`
		Name any    `json:"name"`
	}
	if err := json.Unmarshal(parserFixture(t), &blocks); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, block := range blocks {
		if block.Key == "var" {
			lines = append(lines, "var "+block.Name.(string)+" {")
			continue
		}
		if block.Key != "set" {
			continue
		}
		parts := block.Name.([]any)
		lines = append(lines, fmt.Sprintf("set %s, %s {", parts[0], parts[1]))
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func TestLoadFixtureAndPublicQueries(t *testing.T) {
	fsys := fixtureFS(t)
	oldParser := parseSCL
	parseSCL = func(string) ([]byte, error) { return parserFixture(t), nil }
	t.Cleanup(func() { parseSCL = oldParser })
	a, err := Load(fsys, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	if !a.DeclaresAgents() {
		t.Fatal("DeclaresAgents() = false")
	}
	if got := len(a.Agents()); got != 1 {
		t.Fatalf("Agents() length = %d", got)
	}
	secondAgent := *a.AgentRecords[0]
	secondAgent.DisplayName = "Another Agent"
	a.AgentRecords = append(a.AgentRecords, &secondAgent)
	if got := a.Agents(); len(got) != 2 || got[0].DisplayName != "Another Agent" {
		t.Fatalf("Agents() is not sorted: %#v", got)
	}
	a.AgentRecords = a.AgentRecords[:1]
	if agent, ok := a.Agent("support"); !ok || agent.DisplayName != "Support Agent" {
		t.Fatalf("Agent(support) = %#v, %v", agent, ok)
	}
	if _, ok := a.Agent("missing"); ok {
		t.Fatal("Agent(missing) unexpectedly exists")
	}
	if task, ok := a.TaskType("returns"); !ok || task.Name != "returns" || task.DefaultAgent == nil {
		t.Fatalf("TaskType(returns) = %#v, %v", task, ok)
	}
	if task, ok := a.TaskType("return-s"); ok || task != nil {
		t.Fatalf("TaskType(missing) = %#v, %v", task, ok)
	}
	if skill, ok := a.Skill("refund-policy"); !ok || skill.Title != "Refund policy" {
		t.Fatalf("Skill(refund-policy) = %#v, %v", skill, ok)
	}
	if action, ok := a.Action("find-order"); !ok || action.DisplayName != "Find Order" {
		t.Fatalf("Action(find-order) = %#v, %v", action, ok)
	}
	if _, ok := a.Action("missing"); ok {
		t.Fatal("Action(missing) unexpectedly exists")
	}
	if file, line, ok := a.KeyTaken("agent_support"); !ok || line < 1 || !strings.HasSuffix(file, "records/all.scl") {
		t.Fatalf("KeyTaken() = %q, %d, %v", file, line, ok)
	}
	if _, _, ok := a.KeyTaken("unknown"); ok {
		t.Fatal("unknown key is taken")
	}
	if !a.HasSkillLink("task_type_returns", "persona_support") || !a.HasSkillLink("task_type_returns", "skill_refund_policy") {
		t.Fatal("HasSkillLink() missed a parsed link")
	}
	if a.HasSkillLink("unknown", "persona_support") {
		t.Fatal("HasSkillLink() matched unknown task type")
	}
	if !a.HasToolLink("task_type_returns", ToolRef{ActionKey: "find_order", OwnAction: true}) {
		t.Fatal("HasToolLink() missed own action")
	}
	if !a.HasToolLink("task_type_returns", ToolRef{AppID: PlatformAppID, Name: "process-data"}) {
		t.Fatal("HasToolLink() missed platform tool")
	}
	if a.HasToolLink("unknown", ToolRef{AppID: PlatformAppID, Name: "process-data"}) {
		t.Fatal("HasToolLink() matched unknown kind of work")
	}
	if lesson, ok := a.Lesson("find_order"); !ok || lesson.Title != "Find Order" {
		t.Fatalf("Lesson() = %#v, %v", lesson, ok)
	}
	if _, ok := a.Lesson("unknown"); ok {
		t.Fatal("Lesson(unknown) unexpectedly exists")
	}
	if got := a.NextSkillPosition("task_type_returns"); got != 40 {
		t.Fatalf("NextSkillPosition() = %d", got)
	}
	if got := a.NextSkillPosition("unknown"); got != 10 {
		t.Fatalf("empty NextSkillPosition() = %d", got)
	}
	a.TaskTypeSkillLinks = append(a.TaskTypeSkillLinks, &TaskTypeSkillLink{TaskTypeRef: Reference{Key: "negative"}, Position: -3})
	if got := a.NextSkillPosition("negative"); got != 7 {
		t.Fatalf("negative NextSkillPosition() = %d", got)
	}
	if !a.HasLookup("records/all.scl", "platform_tool_process_data") || !a.HasLookup("apps/com.acme.shop/records/all.scl", "own_action_lookup") {
		t.Fatal("HasLookup() missed a variable")
	}
	if a.HasLookup("records/other.scl", "own_action_lookup") {
		t.Fatal("HasLookup() matched the wrong file")
	}
	if !a.HasLookup("/repo/apps/com.acme.shop/records/all.scl", "own_action_lookup") {
		t.Fatal("HasLookup() missed an absolute path")
	}
	if path, ok := a.StatesFile(); !ok || path != "apps/com.acme.shop/records/task-types/states.json" {
		t.Fatalf("StatesFile() = %q, %v", path, ok)
	}
	if got := (ToolRef{OwnAction: true, Name: "find-order"}).String(); got != "find-order" {
		t.Fatalf("own ToolRef.String() = %q", got)
	}
	if got := (ToolRef{AppID: PlatformAppID, Name: "process-data"}).String(); got != "dev.simple.system/process-data" {
		t.Fatalf("platform ToolRef.String() = %q", got)
	}
	inline := *a.Knowledge[0]
	inline.Body = Body{Inline: "inline body", Readable: true}
	if got := bodyMeaning(&inline); got != "title:"+inline.Title {
		t.Fatalf("inline body meaning = %q", got)
	}
	pathKnowledge := &Knowledge{Key: "other", Title: "Alternate", Body: Body{Path: "knowledge/skill-special.md", Readable: true}}
	a.Knowledge = append(a.Knowledge, pathKnowledge)
	if got, ok := a.Skill("special"); !ok || got != pathKnowledge {
		t.Fatalf("Skill() body path lookup = %#v, %v", got, ok)
	}
	keyKnowledge := &Knowledge{Key: "named", Title: "Named", Body: Body{Inline: "body", Readable: true}}
	a.Knowledge = append(a.Knowledge, keyKnowledge)
	if got, ok := a.Skill("named"); !ok || got != keyKnowledge {
		t.Fatalf("Skill() direct key lookup = %#v, %v", got, ok)
	}
	reference := &Knowledge{Key: "knowledge_return_terms", Title: "Return Terms", Body: Body{Path: "knowledge/return-terms.md", Readable: true}}
	a.Knowledge = append(a.Knowledge, reference)
	if got, ok := a.ReferenceKnowledge("return-terms"); !ok || got != reference {
		t.Fatalf("ReferenceKnowledge() = %#v, %v", got, ok)
	}
	if _, ok := a.ReferenceKnowledge("missing"); ok {
		t.Fatal("ReferenceKnowledge(missing) unexpectedly exists")
	}
}

func TestLoadRecognizesOnlyPlatformTables(t *testing.T) {
	type parsedRecord struct {
		Key      string   `json:"key"`
		Name     []string `json:"name"`
		Children []any    `json:"children"`
	}
	tables := []string{"agent", "dev_simple_system.agent", `"agent"`, `"dev_simple_system.agent"`, "tenant.agent", "agent.agent", "task_type", "dev_simple_system.task_type", `"task_type"`, `"dev_simple_system.task_type"`, "tenant.task_type"}
	blocks := make([]parsedRecord, 0, len(tables))
	var source strings.Builder
	for i, table := range tables {
		key := fmt.Sprintf("table_%d", i)
		blocks = append(blocks, parsedRecord{Key: "set", Name: []string{table, key}, Children: []any{}})
		fmt.Fprintf(&source, "set %s, %s {\n", table, key)
	}
	parsed, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	fsys := newMemoryFS()
	fsys.put("records/tables.scl", []byte(source.String()))
	oldParser := parseSCL
	parseSCL = func(string) ([]byte, error) { return parsed, nil }
	t.Cleanup(func() { parseSCL = oldParser })

	app, err := Load(fsys, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(app.AgentRecords); got != 4 {
		t.Errorf("agent table count = %d, want bare and platform-prefixed quoted and unquoted tables only", got)
	}
	if got := len(app.TaskTypes); got != 4 {
		t.Errorf("kind-of-work table count = %d, want bare and platform-prefixed quoted and unquoted tables only", got)
	}
}

func TestLookupReportsItsToolAndSourceLine(t *testing.T) {
	app := &App{
		RepoRoot: fixtureRepoRoot,
		Dir:      filepath.Join(fixtureRepoRoot, "apps", fixtureAppID),
		lookups: map[string]map[string]lookup{
			"apps/com.acme.shop/records/custom.scl": {
				"platform_tool_process_data": {appID: PlatformAppID, name: "process-data", file: "apps/com.acme.shop/records/custom.scl", line: 9},
			},
		},
	}
	got, ok := app.Lookup(filepath.Join(app.Dir, "records", "custom.scl"), "platform_tool_process_data")
	if !ok || got.AppID != PlatformAppID || got.Name != "process-data" || got.File != "apps/com.acme.shop/records/custom.scl" || got.Line != 9 {
		t.Fatalf("Lookup() = %#v, %v", got, ok)
	}
	if _, ok := app.Lookup("records/other.scl", "platform_tool_process_data"); ok {
		t.Fatal("Lookup() matched a variable from a different file")
	}
}

func TestCheckAtScansActionMarkersWithoutRecords(t *testing.T) {
	fsys := newMemoryFS()
	appDir := filepath.Join(fixtureRepoRoot, "apps", "deployed-copy")
	fsys.files[filepath.Join(appDir, "actions", "prepare", "src", "index.ts")] = []byte("// TODO(simple): finish this action\n")
	problems, err := CheckAt(fsys, fixtureRepoRoot, fixtureAppID, "apps/deployed-copy")
	if err != nil {
		t.Fatal(err)
	}
	if !problemAt(problems, "apps/deployed-copy/actions/prepare/src/index.ts", 1, "still holds a line") {
		t.Fatalf("CheckAt() problems = %#v, want marker from the supplied app directory", problems)
	}
}

func TestCheckSkipsMalformedRecordsWithoutAgentDeclarationsAndKeepsMarkerScan(t *testing.T) {
	tests := []struct {
		name          string
		record        string
		parseErr      string
		actionMarker  bool
		wantParseCall int
		wantProblem   bool
		wantErr       string
	}{
		{name: "unrelated record is ignored", record: "not valid SCL", wantParseCall: 0},
		{name: "unrelated record still gets marker scan", record: "not valid SCL", actionMarker: true, wantParseCall: 0, wantProblem: true},
		{name: "knowledge record alone does not declare agent work", record: "set dev_simple_system.knowledge, terms {\n", wantParseCall: 0},
		{name: "kind of work for people does not declare agent work", record: "set dev_simple_system.task_type, task_type_returns {\n", wantParseCall: 0},
		{name: "agent executor link declares agent work", record: "set dev_simple_system.agent_task_type, link {\n", parseErr: "line 1: parser rejected the record", wantParseCall: 1, wantErr: "other.scl: line 1"},
		{name: "agent record parse error is reported", record: "set dev_simple_system.agent, agent_support {\n", parseErr: "line 1: parser rejected the record", wantParseCall: 1, wantErr: "other.scl: line 1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newMemoryFS()
			f.put("records/other.scl", []byte(test.record))
			if test.actionMarker {
				f.put("actions/prepare/src/main.ts", []byte("// TODO(simple): finish this action\n"))
			}
			parseCalls := 0
			oldParser := parseSCL
			parseSCL = func(string) ([]byte, error) {
				parseCalls++
				message := test.parseErr
				if message == "" {
					message = "parser rejected the record"
				}
				return nil, errors.New(message)
			}
			t.Cleanup(func() { parseSCL = oldParser })

			problems, err := Check(f, fixtureRepoRoot, fixtureAppID)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Check() error = %v, want %q", err, test.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if parseCalls != test.wantParseCall {
				t.Errorf("parser calls = %d, want %d", parseCalls, test.wantParseCall)
			}
			gotProblem := problemAt(problems, "apps/com.acme.shop/actions/prepare/src/main.ts", 1, "still holds a line")
			if gotProblem != test.wantProblem {
				t.Errorf("action marker finding = %v, want %v; findings: %#v", gotProblem, test.wantProblem, problems)
			}
		})
	}
}

func TestAgentDeclarationQuickScanFindsCandidateRecords(t *testing.T) {
	tests := []struct {
		name   string
		record string
		want   bool
	}{
		{name: "prefixed agent", record: "set dev_simple_system.agent, agent_support {\n", want: true},
		{name: "quoted prefixed agent", record: "set \"dev_simple_system.agent\", agent_support {\n", want: true},
		{name: "single-quoted prefixed agent", record: "set 'dev_simple_system.agent', agent_support {\n", want: true},
		{name: "quoted bare agent", record: "set \"agent\", agent_support {\n", want: true},
		{name: "single-quoted bare agent", record: "set 'agent', agent_support {\n", want: true},
		{name: "bare kind of work is not an agent declaration", record: "set task_type, task_type_returns {\n"},
		{name: "quoted kind of work is not an agent declaration", record: "set \"task_type\", task_type_returns {\n"},
		{name: "single-quoted kind of work is not an agent declaration", record: "set 'task_type', task_type_returns {\n"},
		{name: "single-quoted prefixed kind of work is not an agent declaration", record: "set 'dev_simple_system.task_type', task_type_returns {\n"},
		{name: "kind of work with an agent link", record: "set agent_task_type, task_type_returns_default_agent {\n", want: true},
		{name: "single-quoted prefixed kind-of-work agent link", record: "set 'dev_simple_system.agent_task_type', task_type_returns_default_agent {\n", want: true},
		{name: "two statements on one line", record: "set customer, c { name \"x\" } set agent, a {\n", want: true},
		{name: "agent executor link is an agent declaration", record: "set agent_task_type, support_returns {\n", want: true},
		{name: "unrelated record", record: "set customer, customer_one {\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newMemoryFS()
			f.put("records/other.scl", []byte(test.record))
			got, err := hasAgentDeclarationsAt(f, fixtureRepoRoot, fixtureAppID, filepath.Join(fixtureRepoRoot, "apps", fixtureAppID))
			if err != nil {
				t.Fatalf("agent declaration quick scan error = %v", err)
			}
			if got != test.want {
				t.Errorf("HasAgentDeclarations() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCheckUsesParsedAgentRecordKinds(t *testing.T) {
	tests := []struct {
		name         string
		record       string
		parserOutput string
		wantDeclared bool
		wantProblems int
	}{
		{
			name:         "comment mentioning an agent does not enable agent checks",
			record:       "# We do not set agent, ever.\nset task_type, task_type_human {\n  name \"human\"\n}\n",
			parserOutput: `[{"name":["task_type","task_type_human"],"type":"block","key":"set","children":[{"type":"kv","value":"human","key":"name","key_loc":{"line":2,"col":3},"value_loc":{"line":2,"col":8}}],"key_loc":{"line":0,"col":0},"name_loc":{"line":0,"col":31}}]`,
		},
		{
			name:         "description mentioning an agent does not enable agent checks",
			record:       "set task_type, task_type_human {\n  name \"human\"\n  description \"We do not set agent, ever.\"\n}\n",
			parserOutput: `[{"name":["task_type","task_type_human"],"type":"block","key":"set","children":[{"type":"kv","value":"human","key":"name","key_loc":{"line":2,"col":3},"value_loc":{"line":2,"col":8}},{"type":"kv","value":"We do not set agent, ever.","key":"description","key_loc":{"line":3,"col":3},"value_loc":{"line":3,"col":15}}],"key_loc":{"line":1,"col":1},"name_loc":{"line":1,"col":32}}]`,
		},
		{
			name:         "parsed agent record enables agent checks",
			record:       "set agent, agent_support {}\n",
			parserOutput: `[{"name":["agent","agent_support"],"type":"block","key":"set","children":[],"key_loc":{"line":1,"col":1},"name_loc":{"line":1,"col":26}}]`,
			wantDeclared: true,
			wantProblems: 1,
		},
		{
			name:         "parsed agent task link enables agent checks on its own",
			record:       "set agent_task_type, link_people {}\n",
			parserOutput: `[{"name":["agent_task_type","link_people"],"type":"block","key":"set","children":[],"key_loc":{"line":1,"col":1},"name_loc":{"line":1,"col":34}}]`,
			wantDeclared: true,
			wantProblems: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fsys := newMemoryFS()
			fsys.put("records/case.scl", []byte(test.record))
			oldParser := parseSCL
			parseSCL = func(string) ([]byte, error) { return []byte(test.parserOutput), nil }
			t.Cleanup(func() { parseSCL = oldParser })

			problems, err := Check(fsys, fixtureRepoRoot, fixtureAppID)
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if len(problems) != test.wantProblems {
				t.Errorf("Check() returned %d problems, want %d: %#v", len(problems), test.wantProblems, problems)
			}
			declared, err := HasAgentDeclarations(fsys, fixtureRepoRoot, fixtureAppID)
			if err != nil {
				t.Fatalf("HasAgentDeclarations() error = %v", err)
			}
			if declared != test.wantDeclared {
				t.Errorf("HasAgentDeclarations() = %v, want %v", declared, test.wantDeclared)
			}
		})
	}
}

func TestAgentScanFormsDetectedBothScans(t *testing.T) {
	tests := []struct {
		name       string
		record     string
		wantDecl   bool
		wantRecord bool
	}{
		{
			name:       "single-quoted bare agent",
			record:     "set 'agent', support {\n",
			wantDecl:   true,
			wantRecord: true,
		},
		{
			name:   "single-quoted prefixed task_type alone",
			record: "set 'dev_simple_system.task_type', x {\n",
		},
		{
			name:   "single-quoted bare task_type alone",
			record: "set 'task_type', x {\n",
		},
		{
			name:       "single-quoted prefixed agent",
			record:     "set 'dev_simple_system.agent', support {\n",
			wantDecl:   true,
			wantRecord: true,
		},
		{
			name:       "two statements on one line bare agent",
			record:     "set customer, c { name \"x\" } set agent, a {\n",
			wantDecl:   true,
			wantRecord: true,
		},
		{
			name:       "two statements on one line single-quoted agent",
			record:     "set customer, c { name \"x\" } set 'agent', a {\n",
			wantDecl:   true,
			wantRecord: true,
		},
		{
			name:   "two statements on one line single-quoted prefixed task_type alone",
			record: "set customer, c { name \"x\" } set 'dev_simple_system.task_type', x {\n",
		},
		{
			name:       "bare agent",
			record:     "set agent, support {\n",
			wantDecl:   true,
			wantRecord: true,
		},
		{
			name:       "double-quoted bare agent",
			record:     "set \"agent\", support {\n",
			wantDecl:   true,
			wantRecord: true,
		},
		{
			name:       "prefixed agent",
			record:     "set dev_simple_system.agent, support {\n",
			wantDecl:   true,
			wantRecord: true,
		},
		{
			name:       "double-quoted prefixed agent",
			record:     "set \"dev_simple_system.agent\", support {\n",
			wantDecl:   true,
			wantRecord: true,
		},
		{
			name:   "bare task_type alone",
			record: "set task_type, returns {\n",
		},
		{
			name:   "prefixed task_type alone",
			record: "set dev_simple_system.task_type, returns {\n",
		},
		{
			name:   "double-quoted prefixed task_type alone",
			record: "set \"dev_simple_system.task_type\", returns {\n",
		},
		{
			name:       "single-quoted agent executor link counts",
			record:     "set 'agent_task_type', link {\n",
			wantDecl:   true,
			wantRecord: true,
		},
		{
			name:   "single-quoted task_type_skill link",
			record: "set 'task_type_skill', link {\n",
		},
		{
			name:   "single-quoted task_type_tool link",
			record: "set 'task_type_tool', link {\n",
		},
		{
			name:   "single-quoted logic_skill link",
			record: "set 'logic_skill', link {\n",
		},
		{
			name:       "two statements on one line with agent executor link",
			record:     "set customer, c { name \"x\" } set 'agent_task_type', link {\n",
			wantDecl:   true,
			wantRecord: true,
		},
		{
			name:   "knowledge record alone",
			record: "set dev_simple_system.knowledge, terms {\n",
		},
		{
			name:       "unrelated bare record",
			record:     "set customer, c { name \"x\" }\n",
			wantDecl:   false,
			wantRecord: false,
		},
		{
			name:       "unrelated single-quoted record",
			record:     "set 'customer', c { name \"x\" }\n",
			wantDecl:   false,
			wantRecord: false,
		},
		{
			name:       "unrelated double-quoted record",
			record:     "set \"customer\", c { name \"x\" }\n",
			wantDecl:   false,
			wantRecord: false,
		},
		{
			name:       "unrelated var declaration",
			record:     "var platform_tool_lookup { query \"foo\" }\n",
			wantDecl:   false,
			wantRecord: false,
		},
		{
			name:       "empty file",
			record:     "",
			wantDecl:   false,
			wantRecord: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newMemoryFS()
			f.put("records/sample.scl", []byte(test.record))
			gotDecl, err := hasAgentDeclarationsAt(f, fixtureRepoRoot, fixtureAppID, filepath.Join(fixtureRepoRoot, "apps", fixtureAppID))
			if err != nil {
				t.Fatalf("HasAgentDeclarations() error = %v", err)
			}
			if gotDecl != test.wantDecl {
				t.Errorf("HasAgentDeclarations() = %v, want %v", gotDecl, test.wantDecl)
			}
			gotRecord, err := hasAgentRelatedRecordsAt(f, fixtureRepoRoot, fixtureAppID, filepath.Join(fixtureRepoRoot, "apps", fixtureAppID))
			if err != nil {
				t.Fatalf("hasAgentRelatedRecordsAt() error = %v", err)
			}
			if gotRecord != test.wantRecord {
				t.Errorf("hasAgentRelatedRecordsAt() = %v, want %v", gotRecord, test.wantRecord)
			}
		})
	}
}

func TestReaderMapsSingleQuotedDeclarations(t *testing.T) {
	tests := []struct {
		name       string
		recordText string
		parsedName []any
		verify     func(t *testing.T, a *App)
	}{
		{
			name:       "single-quoted bare agent",
			recordText: "set 'agent', support {\n  display_name \"Support Agent\"\n}\n",
			parsedName: []any{"'agent'", "support"},
			verify: func(t *testing.T, a *App) {
				agent, ok := a.Agent("support")
				if !ok || agent == nil {
					t.Fatalf("expected agent 'support' to be mapped to AgentRecords")
				}
				if agent.DisplayName != "Support Agent" {
					t.Errorf("agent.DisplayName = %q, want %q", agent.DisplayName, "Support Agent")
				}
				if agent.Line != 1 {
					t.Errorf("agent.Line = %d, want 1", agent.Line)
				}
			},
		},
		{
			name:       "single-quoted prefixed task_type",
			recordText: "set 'dev_simple_system.task_type', returns {\n  name \"returns\"\n}\n",
			parsedName: []any{"'dev_simple_system.task_type'", "returns"},
			verify: func(t *testing.T, a *App) {
				task, ok := a.TaskType("returns")
				if !ok || task == nil {
					t.Fatalf("expected task_type 'returns' to be mapped to TaskTypes")
				}
				if task.Name != "returns" {
					t.Errorf("task.Name = %q, want %q", task.Name, "returns")
				}
				if task.Line != 1 {
					t.Errorf("task.Line = %d, want 1", task.Line)
				}
			},
		},
		{
			name:       "single-quoted bare task_type",
			recordText: "set 'task_type', triage {\n  name \"triage\"\n}\n",
			parsedName: []any{"'task_type'", "triage"},
			verify: func(t *testing.T, a *App) {
				task, ok := a.TaskType("triage")
				if !ok || task == nil {
					t.Fatalf("expected task_type 'triage' to be mapped to TaskTypes")
				}
				if task.Name != "triage" {
					t.Errorf("task.Name = %q, want %q", task.Name, "triage")
				}
				if task.Line != 1 {
					t.Errorf("task.Line = %d, want 1", task.Line)
				}
			},
		},
		{
			name:       "single-quoted prefixed agent",
			recordText: "set 'dev_simple_system.agent', billing {\n  display_name \"Billing Agent\"\n}\n",
			parsedName: []any{"'dev_simple_system.agent'", "billing"},
			verify: func(t *testing.T, a *App) {
				agent, ok := a.Agent("billing")
				if !ok || agent == nil {
					t.Fatalf("expected agent 'billing' to be mapped to AgentRecords")
				}
				if agent.DisplayName != "Billing Agent" {
					t.Errorf("agent.DisplayName = %q, want %q", agent.DisplayName, "Billing Agent")
				}
				if agent.Line != 1 {
					t.Errorf("agent.Line = %d, want 1", agent.Line)
				}
			},
		},
		{
			name:       "two statements on one line with single-quoted agent",
			recordText: "set customer, c { name \"x\" } set 'agent', support {\n  display_name \"Support Agent\"\n}\n",
			parsedName: []any{"'agent'", "support"},
			verify: func(t *testing.T, a *App) {
				agent, ok := a.Agent("support")
				if !ok || agent == nil {
					t.Fatalf("expected agent 'support' to be mapped to AgentRecords")
				}
				if agent.DisplayName != "Support Agent" {
					t.Errorf("agent.DisplayName = %q, want %q", agent.DisplayName, "Support Agent")
				}
				if agent.Line != 1 {
					t.Errorf("agent.Line = %d, want 1", agent.Line)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newMemoryFS()
			f.put("records/agents.scl", []byte(test.recordText))
			oldParser := parseSCL
			parseSCL = func(string) ([]byte, error) {
				var children []map[string]any
				if strings.Contains(test.recordText, "display_name") {
					name := "Support Agent"
					if strings.Contains(test.recordText, "Billing Agent") {
						name = "Billing Agent"
					}
					children = append(children, map[string]any{"key": "display_name", "value": name})
				}
				if strings.Contains(test.recordText, `name "returns"`) {
					children = append(children, map[string]any{"key": "name", "value": "returns"})
				}
				if strings.Contains(test.recordText, `name "triage"`) {
					children = append(children, map[string]any{"key": "name", "value": "triage"})
				}
				block := map[string]any{
					"key":      "set",
					"name":     test.parsedName,
					"children": children,
				}
				blocks := []any{block}
				if strings.Contains(test.recordText, "set customer, c") {
					blocks = append([]any{map[string]any{
						"key":      "set",
						"name":     []any{"customer", "c"},
						"children": []map[string]any{{"key": "name", "value": "x"}},
					}}, blocks...)
				}
				return json.Marshal(blocks)
			}
			t.Cleanup(func() { parseSCL = oldParser })
			a, err := Load(f, fixtureRepoRoot, fixtureAppID)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			test.verify(t, a)
		})
	}
}

func TestBaseTableAndFindRecordLineSingleQuotes(t *testing.T) {
	baseTableTests := []struct {
		input string
		want  string
	}{
		{"dev_simple_system.agent", "agent"},
		{"knowledge", "knowledge"},
		{"'agent'", "agent"},
		{"\"agent\"", "agent"},
		{"'dev_simple_system.task_type'", "task_type"},
		{"\"dev_simple_system.task_type\"", "task_type"},
		{"'dev_simple_system.agent'", "agent"},
		{"'task_type'", "task_type"},
		{"bare_table", "bare_table"},
	}
	for _, tc := range baseTableTests {
		if got := baseTable(tc.input); got != tc.want {
			t.Errorf("baseTable(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}

	findLineTests := []struct {
		name  string
		data  string
		table string
		key   string
		want  int
	}{
		{"bare table", "set agent, right {}\n", "dev_simple_system.agent", "right", 1},
		{"single-quoted table", "set 'agent', right {}\n", "dev_simple_system.agent", "right", 1},
		{"single-quoted prefixed table", "set 'dev_simple_system.task_type', x {}\n", "task_type", "x", 1},
		{"two statements bare", "set customer, c { name \"x\" } set agent, a {}\n", "agent", "a", 1},
		{"two statements single-quoted", "set customer, c { name \"x\" } set 'agent', a {}\n", "agent", "a", 1},
		{"two statements single-quoted prefixed", "set customer, c { name \"x\" } set 'dev_simple_system.task_type', x {}\n", "task_type", "x", 1},
		{"missing key", "set agent, other {}\n", "agent", "right", 0},
	}
	for _, tc := range findLineTests {
		t.Run(tc.name, func(t *testing.T) {
			if got := findRecordLine([]byte(tc.data), tc.table, tc.key); got != tc.want {
				t.Errorf("findRecordLine(%q, %q, %q) = %d, want %d", tc.data, tc.table, tc.key, got, tc.want)
			}
		})
	}
}

func TestLoadAtAndDeclarationScanFailures(t *testing.T) {
	t.Run("load supplied directory", func(t *testing.T) {
		oldParser := parseSCL
		parseSCL = func(string) ([]byte, error) { return parserFixture(t), nil }
		t.Cleanup(func() { parseSCL = oldParser })
		app, err := LoadAt(fixtureFS(t), fixtureRepoRoot, fixtureAppID, "apps/com.acme.shop")
		if err != nil {
			t.Fatalf("LoadAt() error = %v", err)
		}
		if !app.DeclaresAgents() {
			t.Fatal("LoadAt() did not load agent declarations from the supplied directory")
		}
	})

	tests := []struct {
		name    string
		fsys    *memoryFS
		wantErr string
	}{
		{name: "records listing failure", fsys: func() *memoryFS {
			f := newMemoryFS()
			f.readDirErr[filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, "records")] = errors.New("directory unavailable")
			return f
		}(), wantErr: "failed to list records"},
		{name: "record read failure", fsys: func() *memoryFS {
			f := newMemoryFS()
			f.put("records/unrelated.scl", []byte("set customer, one {\n"))
			f.readErr[filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, "records", "unrelated.scl")] = errors.New("read denied")
			return f
		}(), wantErr: "failed to read apps/com.acme.shop/records/unrelated.scl"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := HasAgentDeclarations(test.fsys, fixtureRepoRoot, fixtureAppID)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("HasAgentDeclarations() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestLoadMissingRecordsAndParserErrors(t *testing.T) {
	tests := []struct {
		name    string
		fsys    *memoryFS
		parser  func(string) ([]byte, error)
		wantErr string
	}{
		{name: "missing records is empty", fsys: newMemoryFS(), parser: func(string) ([]byte, error) { return nil, nil }},
		{name: "parser error names file", fsys: fixtureFS(t), parser: func(string) ([]byte, error) { return nil, errors.New("refused") }, wantErr: "apps/com.acme.shop/records/all.scl"},
		{name: "invalid parser output names file", fsys: fixtureFS(t), parser: func(string) ([]byte, error) { return []byte("{"), nil }, wantErr: "apps/com.acme.shop/records/all.scl"},
		{name: "file read error names file", fsys: func() *memoryFS {
			f := fixtureFS(t)
			f.readErr[filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, "records/all.scl")] = errors.New("disk read failed")
			return f
		}(), parser: func(string) ([]byte, error) { return parserFixture(t), nil }, wantErr: "failed to read apps/com.acme.shop/records/all.scl"},
		{name: "directory read error is wrapped", fsys: func() *memoryFS {
			f := fixtureFS(t)
			f.readDirErr[filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, "records")] = errors.New("directory failed")
			return f
		}(), parser: func(string) ([]byte, error) { return parserFixture(t), nil }, wantErr: "failed to list records"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			old := parseSCL
			parseSCL = test.parser
			t.Cleanup(func() { parseSCL = old })
			a, err := Load(test.fsys, fixtureRepoRoot, fixtureAppID)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Load() error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if a.DeclaresAgents() || len(a.TaskTypes) != 0 {
				t.Fatalf("missing records yielded declarations: %#v", a)
			}
			if _, ok := a.StatesFile(); ok {
				t.Fatal("missing records unexpectedly have a states file")
			}
		})
	}
}

func TestReferenceFormsAndNameConversions(t *testing.T) {
	tests := []struct {
		name, raw string
		want      string
		readable  bool
	}{
		{name: "double quoted record", raw: `$app_record("record_key")`, want: "record_key", readable: true},
		{name: "single quoted record", raw: `$app_record('record_key')`, want: "record_key", readable: true},
		{name: "unsupported expression", raw: `$file('x')`, readable: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := refFor(test.raw, nil, fixtureAppID, nil)
			if got.Key != test.want || got.Readable != test.readable {
				t.Fatalf("refFor() = %#v", got)
			}
		})
	}
	vars := map[string]lookup{"platform": {appID: PlatformAppID, name: "process-data"}, "own": {appID: fixtureAppID, name: "check-order"}}
	for _, test := range []struct {
		name, raw, wantKey string
		own                bool
	}{
		{name: "jq selection", raw: `$var('platform') |> $jq('.logic[] | select(.name == "mutate-data") | .id')`, own: false},
		{name: "query name fallback", raw: `$var("platform") |> $jq('.logic[0].id')`, own: false},
		{name: "same app lookup", raw: `$var('own') |> $jq('.logic[0].id')`, wantKey: "check_order", own: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := refFor(test.raw, vars, fixtureAppID, map[string]string{"check-order": "check_order"})
			if !got.Readable || got.Key != test.wantKey || (got.AppID == fixtureAppID) != test.own {
				t.Fatalf("refFor() = %#v", got)
			}
		})
	}
	if Snake("price-check") != "price_check" || Kebab("price_check") != "price-check" {
		t.Fatal("name conversion failed")
	}
	if got := refFor(42, nil, fixtureAppID, nil); got.Readable {
		t.Fatalf("non-string reference became readable: %#v", got)
	}
}

func TestLoadHandlesBareAndPrefixedTablesAndRecordLines(t *testing.T) {
	fys := fixtureFS(t)
	old := parseSCL
	parseSCL = func(string) ([]byte, error) { return parserFixture(t), nil }
	t.Cleanup(func() { parseSCL = old })
	a, err := Load(fys, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.Skill("refund_policy"); !ok {
		t.Fatal("underscore task name did not match hyphen spelling")
	}
	if _, _, ok := a.KeyTaken("playbook_returns"); !ok {
		t.Fatal("bare table record was not loaded")
	}
	for _, item := range a.TaskTypes {
		if item.Line == 0 {
			t.Errorf("record %q has no source line", item.Key)
		}
	}
}

func TestLoadSkipsMalformedBlocksAndVisitsNestedSCL(t *testing.T) {
	f := newMemoryFS()
	f.put("records/a.scl", []byte("var empty_query {\n"))
	f.put("records/nested/b.scl", []byte("set logic, sample_action {\n"))
	outputs := map[string][]byte{
		filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, "records/a.scl"):        []byte(`[{"key":"set","name":"wrong"},{"key":"set","name":["only_one"]},{"key":"ignored","name":"x"},{"key":"var","name":4},{"key":"var","name":"empty_query","children":[]}]`),
		filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, "records/nested/b.scl"): []byte(`[{"key":"set","name":["logic","sample_action"],"children":[{"key":"name","value":"sample-action"}]}]`),
	}
	old := parseSCL
	parseSCL = func(path string) ([]byte, error) {
		output, ok := outputs[path]
		if !ok {
			return nil, fmt.Errorf("unexpected parser path %s", path)
		}
		return output, nil
	}
	t.Cleanup(func() { parseSCL = old })
	a, err := Load(f, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Actions) != 1 || a.Actions[0].Name != "sample-action" {
		t.Fatalf("nested action = %#v", a.Actions)
	}
	if _, _, ok := a.KeyTaken("sample_action"); !ok {
		t.Fatal("nested SCL record was not indexed")
	}
	if !a.HasLookup("records/a.scl", "empty_query") {
		t.Fatal("var without a query was not indexed")
	}
}

func TestCheckValidFixtureAndAppOnlyTarget(t *testing.T) {
	oldParser := parseSCL
	parseSCL = func(string) ([]byte, error) { return parserFixture(t), nil }
	t.Cleanup(func() { parseSCL = oldParser })
	oldInstructions, oldSkill := checkInstructions, checkSkill
	checkInstructions = func(string, string, string) ([]structure.Finding, []structure.Finding) { return nil, nil }
	checkSkill = func(string) []structure.Finding { return nil }
	t.Cleanup(func() { checkInstructions = oldInstructions; checkSkill = oldSkill })
	fsys := fixtureFS(t)
	problems, err := Check(fsys, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("Check() problems = %#v", problems)
	}
	a, err := Load(fsys, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	a.TaskTypes = nil
	a.Actions = nil
	a.AgentTaskTypeLinks = nil
	a.TaskTypeSkillLinks = nil
	a.TaskTypeToolLinks = nil
	a.LogicSkillLinks = nil
	problems, err = a.check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("app-only target problems = %#v", problems)
	}
}

func TestCheckRuleCases(t *testing.T) {
	tests := []struct {
		name, want string
		mutate     func(*App, *memoryFS)
	}{
		{name: "marker in knowledge", want: "still holds a line", mutate: func(_ *App, f *memoryFS) {
			f.put("knowledge/skill-refund-policy.md", []byte("TODO(simple): write this"))
		}},
		{name: "marker in record", want: "still holds a line", mutate: func(_ *App, f *memoryFS) {
			file := filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, "records/all.scl")
			f.put("records/all.scl", append(f.files[file], []byte("TODO(simple): complete the record\n")...))
		}},
		{name: "marker in shape", want: "still holds a line", mutate: func(_ *App, f *memoryFS) {
			f.put("records/task-types/returns.json", []byte("{\n  \"input\": {},\n  \"output\": null,\n  \"note\": \"TODO(simple): finish\"\n}"))
		}},
		{name: "marker in action source", want: "still holds a line", mutate: func(_ *App, f *memoryFS) {
			f.put("actions/find-order/src/main.ts", []byte("TODO(simple): finish tool\n"))
		}},
		{name: "state words in notes are not declared states", want: "states file does not name queued", mutate: func(_ *App, f *memoryFS) {
			f.put("records/task-types/states.json", []byte(`{"notes":["queued","working","waiting_for_user","waiting_for_event","completed","cancelled","failed"]}`))
		}},
		{name: "agent reference unreadable", want: "unreadable persona reference", mutate: func(a *App, _ *memoryFS) { a.AgentRecords[0].Instructions.Readable = false }},
		{name: "agent reference missing knowledge", want: "no persona body", mutate: func(a *App, _ *memoryFS) { a.AgentRecords[0].Instructions.Key = "missing" }},
		{name: "agent body file missing", want: "persona body cannot be read", mutate: func(a *App, _ *memoryFS) {
			a.AgentRecords[0].Persona.Body = Body{Path: "knowledge/absent.md", Readable: true}
		}},
		{name: "agent persona body absent", want: "no persona body", mutate: func(a *App, _ *memoryFS) { a.AgentRecords[0].Persona.Body = Body{} }},
		{name: "default absent", want: "has no default agent", mutate: func(a *App, _ *memoryFS) {
			a.AgentTaskTypeLinks[0].Role = "reviewer"
			a.AgentTaskTypeLinks[0].IsDefault = false
			a.TaskTypes[0].DefaultAgent = nil
		}},
		{name: "multiple defaults", want: "more than one default agent", mutate: func(a *App, _ *memoryFS) {
			duplicate := *a.AgentTaskTypeLinks[0]
			duplicate.Key = "another_default"
			a.AgentTaskTypeLinks = append(a.AgentTaskTypeLinks, &duplicate)
		}},
		{name: "default reference unreadable", want: "unreadable default agent", mutate: func(a *App, _ *memoryFS) { a.AgentTaskTypeLinks[0].AgentRef.Readable = false }},
		{name: "default is not app agent", want: "not an agent of this app", mutate: func(a *App, _ *memoryFS) { a.AgentTaskTypeLinks[0].AgentRef.Key = "missing" }},
		{name: "agent task type missing", want: "kind of work that does not exist", mutate: func(a *App, _ *memoryFS) { a.AgentTaskTypeLinks[0].TaskTypeRef.Key = "missing" }},
		{name: "agent task type reference unreadable", want: "unreadable kind of work link", mutate: func(a *App, _ *memoryFS) { a.AgentTaskTypeLinks[0].TaskTypeRef.Readable = false }},
		{name: "playbook reference unreadable", want: "unreadable playbook reference", mutate: func(a *App, _ *memoryFS) { a.TaskTypes[0].PlaybookRef.Readable = false }},
		{name: "playbook reference missing", want: "names a playbook without a body", mutate: func(a *App, _ *memoryFS) { a.TaskTypes[0].PlaybookRef.Key = "missing"; a.TaskTypes[0].Playbook = nil }},
		{name: "playbook body absent", want: "names a playbook without a body", mutate: func(a *App, _ *memoryFS) { a.TaskTypes[0].Playbook.Body = Body{} }},
		{name: "persona not carried", want: "does not carry agent", mutate: func(a *App, _ *memoryFS) { a.TaskTypeSkillLinks = removeSkill(a.TaskTypeSkillLinks, "persona_support") }},
		{name: "playbook not carried", want: "does not carry playbook", mutate: func(a *App, _ *memoryFS) {
			a.TaskTypeSkillLinks = removeSkill(a.TaskTypeSkillLinks, "playbook_returns")
		}},
		{name: "inline scalar is not a shape object", want: "shape must be valid JSON with an object at its top level", mutate: func(a *App, _ *memoryFS) { a.records["task_type_returns"].fields["schema"] = "expression" }},
		{name: "shape file without json conversion", want: "no readable shape", mutate: func(a *App, _ *memoryFS) {
			a.records["task_type_returns"].fields["schema"] = "$file('records/task-types/returns.json')"
		}},
		{name: "shape file missing", want: "shape file is missing", mutate: func(_ *App, f *memoryFS) { f.delete("records/task-types/returns.json") }},
		{name: "shape invalid json", want: "valid JSON", mutate: func(_ *App, f *memoryFS) { f.put("records/task-types/returns.json", []byte("{")) }},
		{name: "shape top level is not object", want: "top level", mutate: func(_ *App, f *memoryFS) { f.put("records/task-types/returns.json", []byte("[]")) }},
		{name: "shape input not accepted", want: `input must have "type"`, mutate: func(_ *App, f *memoryFS) {
			f.put("records/task-types/returns.json", []byte(`{"input":false,"output":{"type":"object"}}`))
		}},
		{name: "shape input object with wrong type", want: `input must have "type"`, mutate: func(_ *App, f *memoryFS) {
			f.put("records/task-types/returns.json", []byte(`{"input":{"type":"string"},"output":{"type":"object"}}`))
		}},
		{name: "shape output not object", want: `output must have "type": "object"`, mutate: func(_ *App, f *memoryFS) {
			f.put("records/task-types/returns.json", []byte(`{"input":{},"output":[]}`))
		}},
		{name: "states reference unreadable", want: "unreadable states file reference", mutate: func(a *App, _ *memoryFS) { a.records["task_type_returns"].fields["states"] = "bad expression" }},
		{name: "states file missing", want: "states file is missing", mutate: func(_ *App, f *memoryFS) { f.delete("records/task-types/states.json") }},
		{name: "states invalid json", want: "must be valid JSON", mutate: func(_ *App, f *memoryFS) { f.put("records/task-types/states.json", []byte("{")) }},
		{name: "state missing", want: "does not name failed", mutate: func(_ *App, f *memoryFS) {
			f.put("records/task-types/states.json", []byte(`{"queued":true,"working":true,"waiting_for_user":true,"waiting_for_event":true,"completed":true,"cancelled":true}`))
		}},
		{name: "tool reference unreadable", want: "unreadable tool reference", mutate: func(a *App, _ *memoryFS) { a.TaskTypeToolLinks[0].LogicRef.Readable = false }},
		{name: "tool task type invalid", want: "kind of work that does not exist", mutate: func(a *App, _ *memoryFS) { a.TaskTypeToolLinks[0].TaskTypeRef.Key = "missing" }},
		{name: "own tool does not exist", want: "not an action of this app", mutate: func(a *App, _ *memoryFS) { a.TaskTypeToolLinks[0].Tool.ActionKey = "missing" }},
		{name: "own tool not built", want: "has not been built", mutate: func(_ *App, f *memoryFS) { f.delete("actions/find-order/action.json") }},
		{name: "own action not marked tool", want: "not marked as a tool", mutate: func(_ *App, f *memoryFS) { f.put("actions/find-order/action.json", []byte(`{"ai":{"tool":false}}`)) }},
		{name: "own action json invalid", want: "not marked as a tool", mutate: func(_ *App, f *memoryFS) { f.put("actions/find-order/action.json", []byte("{")) }},
		{name: "unknown platform tool", want: "unknown to this CLI", mutate: func(a *App, _ *memoryFS) { a.TaskTypeToolLinks[1].Tool.Name = "future-tool" }},
		{name: "foreign app lookup", want: "unknown to this CLI", mutate: func(a *App, _ *memoryFS) { a.TaskTypeToolLinks[1].Tool.AppID = "com.other.app" }},
		{name: "task skill task reference unreadable", want: "unreadable knowledge link", mutate: func(a *App, _ *memoryFS) { a.TaskTypeSkillLinks[0].TaskTypeRef.Readable = false }},
		{name: "task skill task missing", want: "kind of work that does not exist", mutate: func(a *App, _ *memoryFS) { a.TaskTypeSkillLinks[0].TaskTypeRef.Key = "missing" }},
		{name: "task skill knowledge unreadable", want: "unreadable knowledge reference", mutate: func(a *App, _ *memoryFS) { a.TaskTypeSkillLinks[2].KnowledgeRef.Readable = false }},
		{name: "task skill knowledge missing", want: "reference to a record that does not exist", mutate: func(a *App, _ *memoryFS) { a.TaskTypeSkillLinks[2].KnowledgeRef.Key = "missing" }},
		{name: "skill body missing", want: `knowledge "Refund policy" has no body`, mutate: func(a *App, _ *memoryFS) { a.Knowledge[2].Body = Body{} }},
		{name: "logic skill action unreadable", want: "unreadable action reference", mutate: func(a *App, _ *memoryFS) { a.LogicSkillLinks[0].LogicRef.Readable = false }},
		{name: "logic skill action missing", want: "refers to an action that does not exist", mutate: func(a *App, _ *memoryFS) { a.LogicSkillLinks[0].LogicRef.Key = "missing" }},
		{name: "logic skill knowledge unreadable", want: "unreadable knowledge reference", mutate: func(a *App, _ *memoryFS) { a.LogicSkillLinks[0].KnowledgeRef.Readable = false }},
		{name: "logic skill knowledge missing", want: "refers to a record that does not exist", mutate: func(a *App, _ *memoryFS) { a.LogicSkillLinks[0].KnowledgeRef.Key = "missing" }},
		{name: "lesson body missing", want: `tool lesson "Find Order" has no body`, mutate: func(a *App, _ *memoryFS) { a.Knowledge[3].Body = Body{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := loadFixtureModel(t)
			f := fixtureFS(t)
			test.mutate(a, f)
			problems, err := a.check(f)
			if err != nil {
				t.Fatal(err)
			}
			if !problemContains(problems, test.want) {
				t.Fatalf("no problem containing %q: %#v", test.want, problems)
			}
		})
	}
}

func TestCheckDefaultExecutorMessagesExplainTaskCreation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*App)
		want   string
	}{
		{
			name: "no default executor",
			mutate: func(a *App) {
				a.AgentTaskTypeLinks[0].Role = "reviewer"
				a.AgentTaskTypeLinks[0].IsDefault = false
			},
			want: `kind of work "returns" has no default agent, so no task can be created for it: mark one executor link as the default`,
		},
		{
			name: "multiple default executors",
			mutate: func(a *App) {
				duplicate := *a.AgentTaskTypeLinks[0]
				duplicate.Key = "another_default"
				a.AgentTaskTypeLinks = append(a.AgentTaskTypeLinks, &duplicate)
			},
			want: `kind of work "returns" has more than one default agent, so no task can be created for it: keep exactly one executor link marked as the default`,
		},
	}
	oldInstructions, oldSkill := checkInstructions, checkSkill
	checkInstructions = func(string, string, string) ([]structure.Finding, []structure.Finding) { return nil, nil }
	checkSkill = func(string) []structure.Finding { return nil }
	t.Cleanup(func() { checkInstructions, checkSkill = oldInstructions, oldSkill })
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := loadFixtureModel(t)
			test.mutate(a)
			problems, err := a.check(fixtureFS(t))
			if err != nil {
				t.Fatalf("check() error = %v", err)
			}
			for _, problem := range problems {
				if strings.Contains(problem.Rule, "default agent") {
					if problem.Rule != test.want {
						t.Fatalf("default executor rule = %q, want %q", problem.Rule, test.want)
					}
					return
				}
			}
			t.Fatalf("no default executor problem: %#v", problems)
		})
	}
}

func TestCheckShapeAllowsNullOutput(t *testing.T) {
	a := loadFixtureModel(t)
	f := fixtureFS(t)
	f.put("records/task-types/returns.json", []byte(`{"input":{},"output":null}`))
	oldInstructions, oldSkill := checkInstructions, checkSkill
	checkInstructions = func(string, string, string) ([]structure.Finding, []structure.Finding) { return nil, nil }
	checkSkill = func(string) []structure.Finding { return nil }
	t.Cleanup(func() { checkInstructions = oldInstructions; checkSkill = oldSkill })
	problems, err := a.check(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range problems {
		if strings.Contains(item.Rule, "shape") {
			t.Fatalf("null output caused a shape problem: %#v", item)
		}
	}
}

func TestCheckShapeAllowsMissingOutput(t *testing.T) {
	a := loadFixtureModel(t)
	f := fixtureFS(t)
	f.put("records/task-types/returns.json", []byte(`{"input":{}}`))
	oldInstructions, oldSkill := checkInstructions, checkSkill
	checkInstructions = func(string, string, string) ([]structure.Finding, []structure.Finding) { return nil, nil }
	checkSkill = func(string) []structure.Finding { return nil }
	t.Cleanup(func() { checkInstructions = oldInstructions; checkSkill = oldSkill })
	problems, err := a.check(f)
	if err != nil || len(problems) != 0 {
		t.Fatalf("missing output was refused: problems=%#v err=%v", problems, err)
	}
}

func TestCheckCapturedInlineShapeParserOutput(t *testing.T) {
	oldInstructions, oldSkill := checkInstructions, checkSkill
	checkInstructions = func(string, string, string) ([]structure.Finding, []structure.Finding) { return nil, nil }
	checkSkill = func(string) []structure.Finding { return nil }
	t.Cleanup(func() { checkInstructions, checkSkill = oldInstructions, oldSkill })
	oldParser := parseSCL
	parseSCL = func(filePath string) ([]byte, error) {
		if strings.HasSuffix(filePath, "agent-link.scl") {
			return []byte(capturedAgentTaskTypeLinkParserOutput), nil
		}
		return []byte(capturedInlineShapeRulesParserOutput), nil
	}
	t.Cleanup(func() { parseSCL = oldParser })

	fsys := fixtureFS(t)
	fsys.put("records/agent-link.scl", []byte("set agent_task_type, link_people {}\n"))
	a, err := Load(fsys, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	schema, ok := a.records["task_type_returns"].fields["schema"].(map[string]any)
	if !ok {
		t.Fatalf("parsed inline schema = %#v, want an object", a.records["task_type_returns"].fields["schema"])
	}
	input, ok := schema["input"].(map[string]any)
	if !ok || input["type"] != "object" {
		t.Fatalf("parsed input shape = %#v, want an object shape", schema["input"])
	}
	properties, ok := input["properties"].(map[string]any)
	if !ok {
		t.Fatalf("parsed input properties = %#v, want an object", input["properties"])
	}
	if _, ok := properties["order_id"].(map[string]any); !ok {
		t.Fatalf("parsed order_id shape = %#v, want an object", properties["order_id"])
	}
	output, ok := schema["output"].(map[string]any)
	if !ok || output["type"] != "object" {
		t.Fatalf("parsed output shape = %#v, want an object shape", schema["output"])
	}
	for _, taskType := range a.TaskTypes {
		if taskType.Name == "returns" {
			a.TaskTypes = []*TaskType{taskType}
			break
		}
	}

	problems, err := a.check(fsys)
	if err != nil {
		t.Fatalf("check() error = %v", err)
	}
	for _, problem := range problems {
		if strings.Contains(problem.Rule, "shape") {
			t.Fatalf("valid inline shape was refused: %#v", problem)
		}
	}
}
func TestCheckShapeFileRules(t *testing.T) {
	const inputRule = `kind of work "returns" shape input must have "type": "object", or be left out, null, true, or an empty object for free input`
	const outputRule = `kind of work "returns" shape output must have "type": "object", or be left out for any answer`
	tests := []struct {
		name        string
		shape       string
		wantProblem string
	}{
		{name: "input omitted", shape: `{"output":{"type":"object"}}`},
		{name: "input null", shape: `{"input":null,"output":{"type":"object"}}`},
		{name: "input true", shape: `{"input":true,"output":{"type":"object"}}`},
		{name: "empty input object", shape: `{"input":{},"output":{"type":"object"}}`},
		{name: "typed input object", shape: `{"input":{"type":"object"},"output":{"type":"object"}}`},
		{name: "output omitted", shape: `{"input":{}}`},
		{name: "output null", shape: `{"input":{},"output":null}`},
		{name: "typed output object", shape: `{"input":{},"output":{"type":"object"}}`},
		{name: "input false", shape: `{"input":false}`, wantProblem: inputRule},
		{name: "input array", shape: `{"input":[]}`, wantProblem: inputRule},
		{name: "input string", shape: `{"input":"free"}`, wantProblem: inputRule},
		{name: "input object missing type", shape: `{"input":{"field":"value"}}`, wantProblem: inputRule},
		{name: "input object wrong type", shape: `{"input":{"type":"string"}}`, wantProblem: inputRule},
		{name: "output false", shape: `{"input":{},"output":false}`, wantProblem: outputRule},
		{name: "output array", shape: `{"input":{},"output":[]}`, wantProblem: outputRule},
		{name: "output string", shape: `{"input":{},"output":"answer"}`, wantProblem: outputRule},
		{name: "output object missing type", shape: `{"input":{},"output":{"field":"value"}}`, wantProblem: outputRule},
		{name: "output object wrong type", shape: `{"input":{},"output":{"type":"string"}}`, wantProblem: outputRule},
	}
	oldInstructions, oldSkill := checkInstructions, checkSkill
	checkInstructions = func(string, string, string) ([]structure.Finding, []structure.Finding) { return nil, nil }
	checkSkill = func(string) []structure.Finding { return nil }
	t.Cleanup(func() { checkInstructions, checkSkill = oldInstructions, oldSkill })
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := loadFixtureModel(t)
			fsys := fixtureFS(t)
			fsys.put("records/task-types/returns.json", []byte(test.shape))
			problems, err := a.check(fsys)
			if err != nil {
				t.Fatalf("check() error = %v", err)
			}
			var shapeProblems []Problem
			for _, problem := range problems {
				if strings.Contains(problem.Rule, "shape") {
					shapeProblems = append(shapeProblems, problem)
				}
			}
			if test.wantProblem == "" {
				if len(shapeProblems) != 0 {
					t.Fatalf("unexpected shape problems: %#v", shapeProblems)
				}
				return
			}
			if len(shapeProblems) != 1 || shapeProblems[0].Rule != test.wantProblem {
				t.Fatalf("shape problems = %#v, want only %q", shapeProblems, test.wantProblem)
			}
		})
	}
}

func TestCheckCapturedInlineShapeRules(t *testing.T) {
	const inputTypeRule = `kind of work "input_bad_type" shape input must have "type": "object", or be left out, null, true, or an empty object for free input`
	const outputTypeRule = `kind of work "output_bad_type" shape output must have "type": "object", or be left out for any answer`
	tests := []struct {
		name        string
		workName    string
		wantProblem string
	}{
		{name: "typed input and output objects", workName: "returns"},
		{name: "input true", workName: "input_true"},
		{name: "empty input object", workName: "input_empty"},
		{name: "input absent", workName: "input_absent"},
		{name: "output absent", workName: "output_absent"},
		{name: "input null is parsed as text", workName: "input_null"},
		{name: "input object has wrong type", workName: "input_bad_type", wantProblem: inputTypeRule},
		{name: "output null is parsed as text", workName: "output_null"},
		{name: "quoted output null is parsed as text", workName: "output_quoted_null"},
		{name: "output object has wrong type", workName: "output_bad_type", wantProblem: outputTypeRule},
	}
	oldInstructions, oldSkill := checkInstructions, checkSkill
	checkInstructions = func(string, string, string) ([]structure.Finding, []structure.Finding) { return nil, nil }
	checkSkill = func(string) []structure.Finding { return nil }
	t.Cleanup(func() { checkInstructions, checkSkill = oldInstructions, oldSkill })
	oldParser := parseSCL
	parseSCL = func(filePath string) ([]byte, error) {
		if strings.HasSuffix(filePath, "agent-link.scl") {
			return []byte(capturedAgentTaskTypeLinkParserOutput), nil
		}
		return []byte(capturedInlineShapeRulesParserOutput), nil
	}
	t.Cleanup(func() { parseSCL = oldParser })
	fsys := fixtureFS(t)
	fsys.put("records/all.scl", []byte(capturedInlineShapeRulesSCL))
	fsys.put("records/agent-link.scl", []byte("set agent_task_type, link_people {}\n"))
	a, err := Load(fsys, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	problems, err := a.check(fsys)
	if err != nil {
		t.Fatalf("check() error = %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var shapeProblems []Problem
			for _, problem := range problems {
				if strings.Contains(problem.Rule, "shape") && strings.Contains(problem.Rule, `kind of work "`+test.workName+`"`) {
					shapeProblems = append(shapeProblems, problem)
				}
			}
			if test.wantProblem == "" {
				if len(shapeProblems) != 0 {
					t.Fatalf("unexpected shape problems: %#v", shapeProblems)
				}
				return
			}
			if len(shapeProblems) != 1 || shapeProblems[0].Rule != test.wantProblem {
				t.Fatalf("shape problems = %#v, want only %q", shapeProblems, test.wantProblem)
			}
		})
	}
}

func TestLoadCapturedNestedBlockList(t *testing.T) {
	tests := []struct {
		name         string
		parserJSON   string
		wantRequired []any
	}{
		{
			name:         "two items in kv_multi unwrapped to plain values",
			parserJSON:   capturedNestedBlockListParserOutput,
			wantRequired: []any{"a", "b"},
		},
		{
			name:         "three items in kv_multi unwrapped to plain values",
			parserJSON:   capturedNestedBlockThreeListParserOutput,
			wantRequired: []any{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldParser := parseSCL
			parseSCL = func(string) ([]byte, error) { return []byte(tt.parserJSON), nil }
			t.Cleanup(func() { parseSCL = oldParser })

			fsys := fixtureFS(t)
			a, err := Load(fsys, fixtureRepoRoot, fixtureAppID)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			schema, ok := a.records["task_type_returns"].fields["schema"].(map[string]any)
			if !ok {
				t.Fatalf("parsed schema = %#v, want map[string]any", a.records["task_type_returns"].fields["schema"])
			}
			input, ok := schema["input"].(map[string]any)
			if !ok {
				t.Fatalf("parsed input = %#v, want map[string]any", schema["input"])
			}
			gotRequired, ok := input["required"].([]any)
			if !ok {
				t.Fatalf("parsed required = %#v, want []any", input["required"])
			}
			if !reflect.DeepEqual(gotRequired, tt.wantRequired) {
				t.Fatalf("input[required] = %#v, want %#v", gotRequired, tt.wantRequired)
			}
		})
	}
}

func TestCheckAgentTaskTypeLinkWithoutOtherAgentRecords(t *testing.T) {
	a := &App{
		ID:       fixtureAppID,
		RepoRoot: fixtureRepoRoot,
		Dir:      filepath.Join(fixtureRepoRoot, "apps", fixtureAppID),
		records:  map[string]record{},
		lookups:  map[string]map[string]lookup{},
		AgentTaskTypeLinks: []*AgentTaskTypeLink{{
			Key: "broken_link", File: "apps/com.acme.shop/records/links.scl", Line: 4,
			AgentRef:    Reference{Key: "missing_agent", Raw: `$app_record("missing_agent")`, Readable: true},
			TaskTypeRef: Reference{Key: "missing_task", Raw: `$app_record("missing_task")`, Readable: true},
		}},
	}
	problems, err := a.check(newMemoryFS())
	if err != nil {
		t.Fatalf("check() error = %v", err)
	}
	if !problemContains(problems, "not an agent of this app") || !problemContains(problems, "kind of work that does not exist in this app") {
		t.Fatalf("check() problems = %#v, want both broken-link findings", problems)
	}
}

func TestCheckWorkWithoutAgentLink(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*App, *memoryFS)
		want   string
	}{
		{name: "does not require default playbook or carried links", mutate: func(a *App, _ *memoryFS) {
			a.AgentTaskTypeLinks = nil
			a.TaskTypeSkillLinks = nil
			a.TaskTypes[0].Playbook = nil
			a.TaskTypes[0].PlaybookRef = Reference{}
		}},
		{name: "named missing playbook is still checked", mutate: func(a *App, _ *memoryFS) {
			a.AgentTaskTypeLinks = nil
			a.TaskTypeSkillLinks = nil
			a.TaskTypes[0].Playbook = nil
			a.TaskTypes[0].PlaybookRef.Key = "missing"
		}, want: "names a playbook without a body"},
		{name: "checks a no-agent knowledge link", mutate: func(a *App, _ *memoryFS) {
			a.AgentTaskTypeLinks = nil
			a.TaskTypes[0].Playbook = nil
			a.TaskTypes[0].PlaybookRef = Reference{}
			link := *a.TaskTypeSkillLinks[0]
			link.KnowledgeRef.Readable = false
			a.TaskTypeSkillLinks = []*TaskTypeSkillLink{&link}
		}, want: "unreadable knowledge reference"},
		{name: "checks a no-agent shape file", mutate: func(a *App, f *memoryFS) {
			a.AgentTaskTypeLinks = nil
			a.TaskTypeSkillLinks = nil
			a.TaskTypes[0].Playbook = nil
			a.TaskTypes[0].PlaybookRef = Reference{}
			f.put("records/task-types/returns.json", []byte(`{"input":[]}`))
		}, want: `input must have "type"`},
		{name: "checks a no-agent tool link", mutate: func(a *App, _ *memoryFS) {
			a.AgentTaskTypeLinks = nil
			a.TaskTypeSkillLinks = nil
			a.TaskTypes[0].Playbook = nil
			a.TaskTypes[0].PlaybookRef = Reference{}
			a.TaskTypeToolLinks[1].Tool.Name = "future-tool"
		}, want: "unknown to this CLI"},
	}
	oldInstructions, oldSkill := checkInstructions, checkSkill
	checkInstructions = func(string, string, string) ([]structure.Finding, []structure.Finding) { return nil, nil }
	checkSkill = func(string) []structure.Finding { return nil }
	t.Cleanup(func() { checkInstructions = oldInstructions; checkSkill = oldSkill })
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := loadFixtureModel(t)
			f := fixtureFS(t)
			test.mutate(a, f)
			problems, err := a.check(f)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" && len(problems) != 0 {
				t.Fatalf("unexpected problems: %#v", problems)
			}
			if test.want != "" && !problemContains(problems, test.want) {
				t.Fatalf("missing problem %q: %#v", test.want, problems)
			}
		})
	}
}

func TestCheckReferenceKnowledgeAndSkillClassification(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		path string
		want string
	}{
		{name: "plain reference knowledge needs no skill structure", body: []byte("Terms used for return decisions.\n"), path: "knowledge/return-terms.md"},
		{name: "empty reference knowledge is refused", body: []byte(" \n"), path: "knowledge/return-terms.md", want: "must contain text"},
		{name: "invalid UTF-8 reference knowledge is refused", body: []byte{0xff}, path: "knowledge/return-terms.md", want: "valid UTF-8"},
		{name: "slug line requests a skill check", body: []byte("# Return Terms\n\nSlug: `skill-return-terms`\n"), path: "knowledge/return-terms.md", want: "add section"},
		{name: "skill file without slug asks to restore it", body: []byte("reference\n"), path: "knowledge/skill-return-terms.md", want: "put the Slug: line back"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a := loadFixtureModel(t)
			knowledge := &Knowledge{Key: "knowledge_return_terms", Title: "Return Terms", Body: Body{Path: test.path, Readable: true}}
			a.Knowledge = append(a.Knowledge, knowledge)
			a.records[knowledge.Key] = record{table: "knowledge", key: knowledge.Key, file: "apps/" + fixtureAppID + "/records/all.scl", line: 100}
			link := &TaskTypeSkillLink{Key: "terms_link", File: "apps/" + fixtureAppID + "/records/all.scl", Line: 101, TaskTypeRef: Reference{Key: "task_type_returns", Readable: true}, KnowledgeRef: Reference{Key: knowledge.Key, Readable: true}}
			f := fixtureFS(t)
			f.put(test.path, test.body)
			var problems []Problem
			add := func(file string, line int, rule string) {
				problems = append(problems, Problem{File: file, Line: line, Rule: strings.TrimSuffix(strings.TrimSpace(rule), ".")})
			}
			a.checkTaskSkillLink(f, link, add)
			if test.want == "" && len(problems) != 0 {
				t.Fatalf("unexpected problems: %#v", problems)
			}
			if test.want != "" && !problemContains(problems, test.want) {
				t.Fatalf("missing problem %q: %#v", test.want, problems)
			}
		})
	}
}

func TestCheckStructureLocationsAndDeduplication(t *testing.T) {
	a := loadFixtureModel(t)
	f := fixtureFS(t)
	oldInstructions, oldSkill := checkInstructions, checkSkill
	checkInstructions = func(_ string, _, _ string) ([]structure.Finding, []structure.Finding) {
		return []structure.Finding{{Line: 2, Rule: "persona finding"}}, []structure.Finding{{Line: 3, Rule: "playbook finding"}}
	}
	checkSkill = func(string) []structure.Finding { return []structure.Finding{{Line: 4, Rule: "skill finding"}} }
	t.Cleanup(func() { checkInstructions = oldInstructions; checkSkill = oldSkill })
	second := *a.TaskTypes[0]
	second.Key = "task_type_followup"
	second.Name = "followup"
	second.File = "apps/com.acme.shop/records/another.scl"
	second.Playbook = a.TaskTypes[0].Playbook
	second.DefaultAgent = a.TaskTypes[0].DefaultAgent
	a.TaskTypes = append(a.TaskTypes, &second)
	defaultLink := *a.AgentTaskTypeLinks[0]
	defaultLink.Key = "followup_default"
	defaultLink.TaskTypeRef.Key = second.Key
	a.AgentTaskTypeLinks = append(a.AgentTaskTypeLinks, &defaultLink)
	personaLink := *a.TaskTypeSkillLinks[0]
	personaLink.TaskTypeRef.Key = second.Key
	a.TaskTypeSkillLinks = append(a.TaskTypeSkillLinks, &personaLink)
	playbookLink := *a.TaskTypeSkillLinks[1]
	playbookLink.TaskTypeRef.Key = second.Key
	a.TaskTypeSkillLinks = append(a.TaskTypeSkillLinks, &playbookLink)
	a.records[second.Key] = record{table: "task_type", key: second.Key, file: second.File, line: 2, fields: map[string]any{"schema": "$file('records/task-types/returns.json') |> $json()", "states": "$file('records/task-types/states.json') |> $json()"}}
	problems, err := a.check(f)
	if err != nil {
		t.Fatal(err)
	}
	if countRule(problems, "persona finding") != 1 {
		t.Fatalf("persona finding repeated: %#v", problems)
	}
	if !problemAt(problems, "apps/com.acme.shop/knowledge/support-persona.md", 2, "persona finding") {
		t.Fatalf("persona finding location missing: %#v", problems)
	}
	if !problemAt(problems, "apps/com.acme.shop/knowledge/playbook-returns.md", 3, "playbook finding") {
		t.Fatalf("playbook finding location missing: %#v", problems)
	}
	if !problemAt(problems, "apps/com.acme.shop/knowledge/skill-refund-policy.md", 4, "skill finding") {
		t.Fatalf("skill finding location missing: %#v", problems)
	}
}

func TestCheckInlineStructureFindingUsesRecordLine(t *testing.T) {
	a := loadFixtureModel(t)
	f := fixtureFS(t)
	a.AgentRecords[0].Persona.Body = Body{Inline: "inline", Readable: true}
	a.AgentRecords[0].Persona.File = "apps/com.acme.shop/records/agent.scl"
	a.AgentRecords[0].Persona.Line = 12
	old := checkInstructions
	checkInstructions = func(string, string, string) ([]structure.Finding, []structure.Finding) {
		return []structure.Finding{{Line: 6, Rule: "inline finding"}}, nil
	}
	t.Cleanup(func() { checkInstructions = old })
	problems, err := a.check(f)
	if err != nil {
		t.Fatal(err)
	}
	if !problemAt(problems, "apps/com.acme.shop/records/agent.scl", 12, "inline finding") {
		t.Fatalf("inline finding location = %#v", problems)
	}
}

func TestCanonicalIgnoresRecordKeysFilesCommentsAndOrder(t *testing.T) {
	left, right := loadFixtureModel(t), loadFixtureModel(t)
	left.AgentRecords[0].File = "apps/com.acme.shop/records/71_agents.scl"
	right.AgentRecords[0].File = "apps/com.acme.shop/records/other.scl"
	left.TaskTypes[0].File = "apps/com.acme.shop/records/71_agents.scl"
	right.TaskTypes[0].File = "apps/com.acme.shop/records/other.scl"
	right.AgentRecords[0].Key = "human_support"
	right.AgentRecords[0].Persona.Key = "persona_different"
	right.AgentRecords[0].Instructions.Key = "persona_different"
	right.TaskTypes[0].Key = "kind_return"
	right.TaskTypes[0].Playbook.Key = "playbook_different"
	right.TaskTypes[0].PlaybookRef.Key = "playbook_different"
	right.TaskTypes[0].DefaultAgentRef.Key = "human_support"
	right.TaskTypes[0].DefaultAgent = right.AgentRecords[0]
	for _, link := range right.AgentTaskTypeLinks {
		link.AgentRef.Key = "human_support"
		link.TaskTypeRef.Key = "kind_return"
	}
	for _, link := range right.TaskTypeSkillLinks {
		link.TaskTypeRef.Key = "kind_return"
		if link.KnowledgeRef.Key == "persona_support" {
			link.KnowledgeRef.Key = "persona_different"
		}
		if link.KnowledgeRef.Key == "playbook_returns" {
			link.KnowledgeRef.Key = "playbook_different"
		}
		if k := right.knowledgeByKey(link.KnowledgeRef.Key); k != nil {
			link.KnowledgeRef.Key = k.Key
		}
	}
	for _, item := range right.Knowledge {
		if item.Key == "persona_support" {
			item.Key = "persona_different"
		}
		if item.Key == "playbook_returns" {
			item.Key = "playbook_different"
		}
	}
	for _, link := range right.TaskTypeSkillLinks {
		if link.KnowledgeRef.Key == "persona_support" {
			link.KnowledgeRef.Key = "persona_different"
		}
		if link.KnowledgeRef.Key == "playbook_returns" {
			link.KnowledgeRef.Key = "playbook_different"
		}
	}
	right.TaskTypeSkillLinks[2].TaskTypeRef.Key = "kind_return"
	right.TaskTypeToolLinks[0].TaskTypeRef.Key = "kind_return"
	right.TaskTypeToolLinks[1].TaskTypeRef.Key = "kind_return"
	right.LogicSkillLinks[0].KnowledgeRef.Key = "tool_find_order"
	remapped := make(map[string]record, len(right.records))
	keyNames := map[string]string{"agent_support": "human_support", "task_type_returns": "kind_return", "persona_support": "persona_different", "playbook_returns": "playbook_different"}
	for key, item := range right.records {
		if replacement, ok := keyNames[key]; ok {
			item.key = replacement
			remapped[replacement] = item
		} else {
			remapped[key] = item
		}
	}
	right.records = remapped
	sort.Slice(right.TaskTypeSkillLinks, func(i, j int) bool {
		return right.TaskTypeSkillLinks[i].Position > right.TaskTypeSkillLinks[j].Position
	})
	if left.Canonical() != right.Canonical() {
		t.Fatalf("canonical descriptions differ:\nleft:\n%s\nright:\n%s", left.Canonical(), right.Canonical())
	}
}

func TestCanonicalIncludesReferenceKnowledgeLink(t *testing.T) {
	knowledge := &Knowledge{Key: "knowledge_return_terms", Body: Body{Inline: "standing terms", Readable: true}}
	a := &App{
		TaskTypes:          []*TaskType{{Key: "task_type_returns", Name: "returns"}},
		Knowledge:          []*Knowledge{knowledge},
		records:            map[string]record{"knowledge_return_terms": {table: "knowledge", key: "knowledge_return_terms"}},
		TaskTypeSkillLinks: []*TaskTypeSkillLink{{TaskTypeRef: Reference{Key: "task_type_returns"}, KnowledgeRef: Reference{Key: knowledge.Key, Readable: true}, Position: 30}},
	}
	want := `["knowledge-link","returns","inline:standing terms","30"]`
	if !strings.Contains(a.Canonical(), want) {
		t.Fatalf("Canonical() = %s, want %s", a.Canonical(), want)
	}
	if got := a.NextSkillPosition("task_type_returns"); got != 40 {
		t.Fatalf("NextSkillPosition() after reference knowledge = %d, want 40", got)
	}
}

func TestKnowledgeNameUsesDeveloperNames(t *testing.T) {
	tests := []struct {
		name     string
		item     *Knowledge
		wantName string
	}{
		{name: "title", item: &Knowledge{Key: "knowledge_terms", Title: "Return Terms"}, wantName: "Return Terms"},
		{name: "body file", item: &Knowledge{Key: "skill_terms", Body: Body{Path: "knowledge/skill-return-terms.md"}}, wantName: "return-terms"},
		{name: "record name", item: &Knowledge{Key: "knowledge_return_terms"}, wantName: "return-terms"},
		{name: "generic fallback", item: &Knowledge{}, wantName: "knowledge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := knowledgeName(tt.item); got != tt.wantName {
				t.Fatalf("knowledgeName() = %q, want %q", got, tt.wantName)
			}
		})
	}
}

func TestProblemOrderingAndRuleSentence(t *testing.T) {
	got := uniqueProblems([]Problem{{File: "b", Line: 3, Rule: "b"}, {File: "a", Line: 2, Rule: "z"}, {File: "a", Line: 1, Rule: "a"}, {File: "a", Line: 1, Rule: "a"}})
	if len(got) != 3 || got[0].File != "a" || got[1].Line != 2 || got[2].File != "b" {
		t.Fatalf("uniqueProblems() = %#v", got)
	}
	if ensureSentence("A plain sentence.") != "A plain sentence" || ensureSentence("Already!") != "Already!" {
		t.Fatal("ensureSentence() did not remove a trailing full stop")
	}
	if ensureSentence("") != "" {
		t.Fatal("ensureSentence(empty) should stay empty")
	}
}

func TestParserAndPathHelpers(t *testing.T) {
	tests := []struct {
		name     string
		body     any
		readable bool
		path     string
	}{
		{name: "file body", body: "$file('knowledge/guide.md')", readable: true, path: "knowledge/guide.md"},
		{name: "inline body", body: "Some inline text", readable: true},
		{name: "unreadable expression", body: "$var('body')", readable: false},
		{name: "non-string body", body: 3, readable: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseBody(test.body)
			if got.Readable != test.readable || got.Path != test.path {
				t.Fatalf("parseBody() = %#v", got)
			}
		})
	}
	if path, ok := parseFilePath("$file('shape.json') |> $json()"); !ok || path != "shape.json" {
		t.Fatalf("parseFilePath() = %q,%v", path, ok)
	}
	for _, raw := range []any{"$file('shape.json')", "$file('shape.json') |> $trim()", 4} {
		if _, ok := parseFilePath(raw); ok {
			t.Errorf("parseFilePath(%v) unexpectedly succeeded", raw)
		}
	}
	if baseTable("dev_simple_system.agent") != "agent" || baseTable("knowledge") != "knowledge" {
		t.Fatal("baseTable() failed")
	}
	if findRecordLine([]byte("set agent, right {}\n"), "dev_simple_system.agent", "right") != 1 {
		t.Fatal("findRecordLine() missed a prefixed/bare table match")
	}
	if findRecordLine([]byte("set agent, other {}\n"), "agent", "right") != 0 {
		t.Fatal("findRecordLine() matched a missing key")
	}
	if str(7) != "" || num(float64(4)) != 4 || num(5) != 5 || num(json.Number("6")) != 6 || num("bad") != 0 || boolValue(true) != true || boolValue("true") {
		t.Fatal("scalar helpers did not preserve parsed values")
	}
	if first("", "second", "third") != "second" || first() == "x" {
		t.Fatal("first() failed")
	}
	if _, ok := resolveRecord(map[string]record{}, Reference{Key: "missing", Readable: true}); ok {
		t.Fatal("resolveRecord() found a missing record")
	}
	if _, ok := resolveRecord(nil, Reference{Key: "missing"}); ok {
		t.Fatal("resolveRecord() accepted an unreadable reference")
	}
	if !sameFile("apps/com.acme.shop/records/a.scl", "records/a.scl", "/repo/apps/com.acme.shop", "/repo") {
		t.Fatal("sameFile() missed an app-relative path")
	}
	if !sameFile("apps/com.acme.shop/records/a.scl", "/repo/apps/com.acme.shop/records/a.scl", "/repo/apps/com.acme.shop", "/repo") {
		t.Fatal("sameFile() missed an absolute repository path")
	}
	if sameFile("apps/com.acme.shop/records/a.scl", "/other/records/a.scl", "/repo/apps/com.acme.shop", "/repo") {
		t.Fatal("sameFile() matched an unrelated absolute path")
	}
}

func TestStatesFileAbsentAndCheckLoadError(t *testing.T) {
	a := &App{}
	if _, ok := a.StatesFile(); ok {
		t.Fatal("StatesFile() succeeded without a kind of work")
	}
	old := parseSCL
	parseSCL = func(string) ([]byte, error) { return nil, errors.New("refused") }
	t.Cleanup(func() { parseSCL = old })
	if _, err := Check(fixtureFS(t), fixtureRepoRoot, fixtureAppID); err == nil || !strings.Contains(err.Error(), "records/all.scl") {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestCheckReadFailuresAndMarkerLineTrimming(t *testing.T) {
	oldParser := parseSCL
	parseSCL = func(string) ([]byte, error) { return parserFixture(t), nil }
	t.Cleanup(func() { parseSCL = oldParser })
	f := fixtureFS(t)
	longLine := "  " + Marker + strings.Repeat("x", 140)
	f.put("knowledge/skill-refund-policy.md", []byte(longLine))
	a, err := Load(f, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	problems, err := a.check(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range problems {
		if strings.Contains(problem.Rule, "still holds a line") {
			line := strings.Split(problem.Rule, `"`)[3]
			if len([]rune(line)) != 100 {
				t.Fatalf("marker quote length = %d", len([]rune(line)))
			}
			break
		}
	}
	f = fixtureFS(t)
	f.readErr[filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, "knowledge/support-persona.md")] = errors.New("body read failed")
	a, err = Load(f, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.check(f); err == nil || !strings.Contains(err.Error(), "failed to read apps/com.acme.shop/knowledge/support-persona.md") {
		t.Fatalf("check marker read error = %v", err)
	}
	f = fixtureFS(t)
	f.readErr[filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, "actions/find-order/action.json")] = errors.New("action read failed")
	a, err = Load(f, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.check(f); err == nil || !strings.Contains(err.Error(), "failed to read apps/com.acme.shop/actions/find-order/action.json") {
		t.Fatalf("action read error = %v", err)
	}
	f = fixtureFS(t)
	f.readDirErr[filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, "knowledge")] = errors.New("directory read failed")
	a, err = Load(f, fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.check(f); err == nil || !strings.Contains(err.Error(), "failed to list apps/com.acme.shop/knowledge") {
		t.Fatalf("marker directory error = %v", err)
	}
	if markerFile("unknown", "file.scl") || markerFile("actions", "actions/a/action.json") || markerFile("records", "records/readme.txt") || markerFile("knowledge", "knowledge/readme.txt") {
		t.Fatal("markerFile() accepted a file outside the marker rules")
	}
}

func loadFixtureModel(t *testing.T) *App {
	t.Helper()
	old := parseSCL
	parseSCL = func(string) ([]byte, error) { return parserFixture(t), nil }
	t.Cleanup(func() { parseSCL = old })
	a, err := Load(fixtureFS(t), fixtureRepoRoot, fixtureAppID)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func fixtureFS(t *testing.T) *memoryFS {
	t.Helper()
	f := newMemoryFS()
	f.put("records/all.scl", fixtureSource(t))
	f.put("records/task-types/returns.json", []byte(`{"input":{},"output":{"type":"object"}}`))
	f.put("records/task-types/states.json", []byte(`{"queued":true,"working":true,"waiting_for_user":true,"waiting_for_event":true,"completed":true,"cancelled":true,"failed":true}`))
	f.put("knowledge/support-persona.md", []byte("# Persona\nBe useful.\n"))
	f.put("knowledge/playbook-returns.md", []byte("# Playbook\nFollow steps.\n"))
	f.put("knowledge/skill-refund-policy.md", []byte("# Skill\n\nSlug: `skill-refund-policy`\n\nUse carefully.\n"))
	f.put("knowledge/tool-find-order.md", []byte("# Tool lesson\n\nSlug: `tool-find-order`\n\nFind a record.\n"))
	f.put("actions/find-order/action.json", []byte(`{"ai":{"tool":true}}`))
	f.put("actions/find-order/src/main.ts", []byte("export {};\n"))
	return f
}

func removeSkill(items []*TaskTypeSkillLink, key string) []*TaskTypeSkillLink {
	result := items[:0]
	for _, item := range items {
		if item.KnowledgeRef.Key != key {
			result = append(result, item)
		}
	}
	return result
}
func problemContains(items []Problem, part string) bool {
	for _, item := range items {
		if strings.Contains(item.Rule, part) {
			return true
		}
	}
	return false
}
func problemAt(items []Problem, file string, line int, part string) bool {
	for _, item := range items {
		if item.File == file && item.Line == line && strings.Contains(item.Rule, part) {
			return true
		}
	}
	return false
}
func countRule(items []Problem, part string) int {
	total := 0
	for _, item := range items {
		if strings.Contains(item.Rule, part) {
			total++
		}
	}
	return total
}

type memoryFS struct {
	files      map[string][]byte
	readErr    map[string]error
	readDirErr map[string]error
}

func newMemoryFS() *memoryFS {
	return &memoryFS{files: map[string][]byte{}, readErr: map[string]error{}, readDirErr: map[string]error{}}
}
func (f *memoryFS) path(name string) string { return filepath.Clean(name) }
func (f *memoryFS) put(name string, data []byte) {
	f.files[f.path(filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, name))] = append([]byte(nil), data...)
}
func (f *memoryFS) delete(name string) {
	delete(f.files, f.path(filepath.Join(fixtureRepoRoot, "apps", fixtureAppID, name)))
}
func (f *memoryFS) Stat(name string) (fs.FileInfo, error) {
	name = f.path(name)
	if _, ok := f.files[name]; ok {
		return fakeInfo(filepath.Base(name), false), nil
	}
	for file := range f.files {
		if strings.HasPrefix(file, name+string(filepath.Separator)) {
			return fakeInfo(filepath.Base(name), true), nil
		}
	}
	return nil, os.ErrNotExist
}
func (f *memoryFS) MkdirAll(string, os.FileMode) error { return nil }
func (f *memoryFS) WriteFile(name string, data []byte, _ os.FileMode) error {
	f.files[f.path(name)] = append([]byte(nil), data...)
	return nil
}
func (f *memoryFS) ReadFile(name string) ([]byte, error) {
	name = f.path(name)
	if err := f.readErr[name]; err != nil {
		return nil, err
	}
	data, ok := f.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}
func (f *memoryFS) ReadDir(name string) ([]os.DirEntry, error) {
	name = f.path(name)
	if err := f.readDirErr[name]; err != nil {
		return nil, err
	}
	children := map[string]bool{}
	for file := range f.files {
		if !strings.HasPrefix(file, name+string(filepath.Separator)) {
			continue
		}
		rest := strings.TrimPrefix(file, name+string(filepath.Separator))
		part := strings.SplitN(rest, string(filepath.Separator), 2)
		children[part[0]] = len(part) > 1
	}
	if len(children) == 0 {
		return nil, os.ErrNotExist
	}
	names := make([]string, 0, len(children))
	for child := range children {
		names = append(names, child)
	}
	sort.Strings(names)
	result := make([]os.DirEntry, 0, len(names))
	for _, child := range names {
		result = append(result, fakeEntry{name: child, dir: children[child]})
	}
	return result, nil
}
func (f *memoryFS) Remove(name string) error { delete(f.files, f.path(name)); return nil }

type fakeEntry struct {
	name string
	dir  bool
}

func (f fakeEntry) Name() string { return f.name }
func (f fakeEntry) IsDir() bool  { return f.dir }
func (f fakeEntry) Type() fs.FileMode {
	if f.dir {
		return fs.ModeDir
	}
	return 0
}
func (f fakeEntry) Info() (fs.FileInfo, error) { return fakeInfo(f.name, f.dir), nil }

type fakeFileInfo struct {
	name string
	dir  bool
}

func fakeInfo(name string, dir bool) fs.FileInfo { return fakeFileInfo{name: name, dir: dir} }
func (f fakeFileInfo) Name() string              { return f.name }
func (f fakeFileInfo) Size() int64               { return 0 }
func (f fakeFileInfo) Mode() os.FileMode {
	if f.dir {
		return os.ModeDir | 0755
	}
	return 0644
}
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }
